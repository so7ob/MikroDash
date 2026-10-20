package subscriber

import (
	"context"
	"encoding/json"
	"net"
	"reflect"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

func disconnectNAS(t *testing.T, svc *RadiusService, code radius.Code) <-chan *radius.Packet {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	packets := make(chan *radius.Packet, 8)
	nas := &radius.PacketServer{SecretSource: radius.StaticSecretSource([]byte(testSecret)), Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		packets <- r.Packet
		_ = w.Write(r.Response(code))
	})}
	go nas.Serve(conn)
	t.Cleanup(func() { _ = nas.Shutdown(context.Background()) })
	svc.Clients[0].DisconnectPort = conn.LocalAddr().(*net.UDPAddr).Port
	return packets
}

func disconnectFixture(t *testing.T) (*Store, *RadiusService, Account, Enforcement, time.Time) {
	t.Helper()
	s, svc, a, grant, at := liveFixture(t)
	if err := s.ObserveRouter(t.Context(), observation(svc, a, grant, at, at), svc.Clients); err != nil {
		t.Fatal(err)
	}
	e := Enforcement{LeaseID: grant.LeaseID, AccountID: a.ID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Reason: "synthetic disconnect acceptance"}
	return s, svc, a, e, at
}

func TestDisconnectUsesConfirmedAddressAndRetainsCreditUntilStop(t *testing.T) {
	for _, code := range []radius.Code{radius.CodeDisconnectACK, radius.CodeDisconnectNAK} {
		t.Run(code.String(), func(t *testing.T) {
			s, svc, account, e, at := disconnectFixture(t)
			svc.VerifySession = func(ctx context.Context, v Enforcement) bool {
				return s.VerifyObservedSession(ctx, v, "hs-lab", time.Now())
			}
			packets := disconnectNAS(t, svc, code)
			before, err := s.Snapshot(t.Context(), account.ID, at)
			if err != nil {
				t.Fatal(err)
			}
			e.Username = "untrusted-caller-value"
			err = svc.Disconnect(t.Context(), e, true)
			if (err == nil) != (code == radius.CodeDisconnectACK) {
				t.Fatal("incorrect acknowledgement result", err)
			}
			select {
			case p := <-packets:
				addresses, err := rfc2865.FramedIPAddress_Gets(p)
				if err != nil || len(addresses) != 1 || addresses[0].String() != "198.51.100.2" || p.Code != radius.CodeDisconnectRequest || rfc2865.UserName_GetString(p) != account.Username || rfc2865.CallingStationID_GetString(p) != e.Device || rfc2866.AcctSessionID_GetString(p) != e.SessionID {
					t.Fatal("Disconnect did not target the API-confirmed owned session", err)
				}
			default:
				t.Fatal("production path sent no packet")
			}
			after, err := s.Snapshot(t.Context(), account.ID, at)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("ACK/NAK changed usage, identity, expiry or reservations before Stop", err)
			}
			var requested, completed int
			result := "session.disconnect.failed"
			if code == radius.CodeDisconnectACK {
				result = "session.disconnect.acknowledged"
			}
			if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='session.disconnect.requested' AND target=?`, e.LeaseID).Scan(&requested); err != nil {
				t.Fatal(err)
			}
			if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action=? AND target=?`, result, e.LeaseID).Scan(&completed); err != nil || requested != 1 || completed != 1 {
				t.Fatal("disconnect audit missing", requested, completed, err)
			}
		})
	}
}

func TestDisconnectRefusesUnconfirmedTargetsBeforeNetworkIO(t *testing.T) {
	cases := []struct {
		name   string
		change func(*ObservedSession, *time.Time, *bool)
	}{
		{"missing-address", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Address = "" }},
		{"malformed-address", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Address = "unknown" }},
		{"unspecified-address", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Address = "0.0.0.0" }},
		{"loopback-address", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Address = "127.0.0.1" }},
		{"multicast-address", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Address = "224.0.0.1" }},
		{"broadcast-address", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Address = "255.255.255.255" }},
		{"ipv6-address", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Address = "2001:db8::2" }},
		{"stale-observation", func(v *ObservedSession, at *time.Time, _ *bool) { v.Seconds = 24; *at = at.Add(-6 * time.Second) }},
		{"future-observation", func(v *ObservedSession, at *time.Time, _ *bool) { v.Seconds = 3; *at = at.Add(3 * time.Second) }},
		{"absent-device", func(_ *ObservedSession, _ *time.Time, present *bool) { *present = false }},
		{"missing-api-id", func(v *ObservedSession, _ *time.Time, _ *bool) { v.ID = "" }},
		{"wrong-account", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Username = "another-account" }},
		{"wrong-device", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Device = "another-device" }},
		{"wrong-network", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Server = "another-hotspot" }},
		{"reused-device-session", func(v *ObservedSession, _ *time.Time, _ *bool) { v.Seconds = 100 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, svc, _, e, _ := disconnectFixture(t)
			packets := disconnectNAS(t, svc, radius.CodeDisconnectACK)
			row, at, present, err := s.observation(t.Context(), e.LeaseID)
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&row, &at, &present)
			if tc.name == "stale-observation" {
				// Preserve an exact start-time match so only freshness can refuse
				// this valid, but old, device observation.
				if _, err = s.sql.Exec(`UPDATE subscriber_leases SET started_at=? WHERE id=?`, at.Add(-24*time.Second).Unix(), e.LeaseID); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.sql.Exec(`UPDATE subscriber_observations SET payload=?, observed_at=?, present=? WHERE lease_id=?`, string(raw), at.Unix(), present, e.LeaseID); err != nil {
				t.Fatal(err)
			}
			// A permissive callback cannot bypass the address and ownership fence.
			svc.VerifySession = func(context.Context, Enforcement) bool { return true }
			if err = svc.Disconnect(t.Context(), e, true); err == nil {
				t.Fatal("unconfirmed target accepted")
			}
			select {
			case <-packets:
				t.Fatal("unconfirmed target reached NAS")
			default:
			}
			var intents int
			if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='session.disconnect.requested'`).Scan(&intents); err != nil || intents != 0 {
				t.Fatal("unconfirmed target recorded a network intent", intents, err)
			}
		})
	}
}

func TestDisconnectRefusesWithoutAuthorityOwnershipOrAudit(t *testing.T) {
	for _, scenario := range []string{"shadow", "disabled", "no-grant", "callback-refused", "missing-observation", "wrong-account", "wrong-network", "wrong-device", "wrong-session", "stopped-lease", "failed-audit"} {
		t.Run(scenario, func(t *testing.T) {
			s, svc, _, e, _ := disconnectFixture(t)
			packets := disconnectNAS(t, svc, radius.CodeDisconnectACK)
			allowed := true
			switch scenario {
			case "shadow", "disabled":
				svc.Mode = scenario
			case "no-grant":
				allowed = false
			case "callback-refused":
				svc.VerifySession = func(context.Context, Enforcement) bool { return false }
			case "wrong-account":
				e.AccountID = "another-account"
			case "wrong-network":
				e.Network = "another-network"
			case "wrong-device":
				e.Device = "another-device"
			case "wrong-session":
				e.SessionID = "another-session"
			case "missing-observation":
				if _, err := s.sql.Exec(`DELETE FROM subscriber_observations`); err != nil {
					t.Fatal(err)
				}
			case "stopped-lease":
				if _, err := s.sql.Exec(`UPDATE subscriber_leases SET state='stopped' WHERE id=?`, e.LeaseID); err != nil {
					t.Fatal(err)
				}
			case "failed-audit":
				if _, err := s.sql.Exec(`CREATE TRIGGER synthetic_audit_failure BEFORE INSERT ON subscriber_audit BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if err := svc.Disconnect(t.Context(), e, allowed); err == nil {
				t.Fatal("unsafe disconnect accepted")
			}
			select {
			case <-packets:
				t.Fatal("unauthorized or unaudited write reached NAS")
			default:
			}
		})
	}
}

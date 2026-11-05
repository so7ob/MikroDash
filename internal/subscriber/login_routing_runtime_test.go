package subscriber

import (
	"context"
	"errors"
	"layeh.com/radius"
	"mikrodash/internal/routeros"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestDomainRoutingCacheFailsClosedAndRetainsKnownEvidence(t *testing.T) {
	r, n, target := routingFixture()
	r.ObservedAt = time.Now()
	n.RouterID = "lab"
	n.LoginRouting = &target
	n.Capabilities = Capabilities{Version: r.Version, Evidence: "synthetic route observation"}
	calls := 0
	failed := false
	service := &RadiusService{Clients: []NAS{n}, ReadLoginRouting: func(ctx context.Context, router string, legacy bool) (LoginRoutingObservation, error) {
		calls++
		if router != "lab" || legacy {
			t.Error("unexpected shared routing scope", router, legacy)
		}
		if failed {
			return LoginRoutingObservation{}, errors.New("synthetic API failure")
		}
		return LoginRoutingObservation{Readiness: r, LegacyComplete: true, LegacyNames: []string{}}, nil
	}}
	if service.LoginRoutingStatus(n, r.ObservedAt).Quality != "unavailable" {
		t.Fatal("cold routing advertised fresh metadata")
	}
	if service.LoginRoutingAllows(n, "pilot", r.ObservedAt) {
		t.Fatal("cold routing granted access")
	}
	if err := service.RefreshLoginRouting(t.Context(), "lab"); err != nil {
		t.Fatal(err)
	}
	if service.LoginRoutingStatus(n, r.ObservedAt).Quality != "fresh" {
		t.Fatal("fresh metadata status unavailable")
	}
	if !service.LoginRoutingAllows(n, "pilot", r.ObservedAt.Add(time.Second)) {
		t.Fatal("fresh single-authority observation refused")
	}
	if service.LoginRoutingAllows(n, "pilot", r.ObservedAt.Add(16*time.Second)) || service.LoginRoutingAllows(n, "pilot", r.ObservedAt.Add(-2*time.Second)) {
		t.Fatal("stale or future observation authorized")
	}
	wrong := n
	wrong.Capabilities.Version = "different-version"
	if service.LoginRoutingAllows(wrong, "pilot", r.ObservedAt) {
		t.Fatal("unqualified NAS version authorized")
	}
	failed = true
	if err := service.RefreshLoginRouting(t.Context(), "lab"); err == nil {
		t.Fatal("API failure hidden")
	}
	if service.LoginRoutingAllows(n, "pilot", r.ObservedAt) {
		t.Fatal("last known data was reused after a failed read")
	}
	if service.loginRoutes["lab"].Observation.Readiness.Version != r.Version {
		t.Fatal("last known evidence was destroyed")
	}
	if service.LoginRoutingStatus(n, r.ObservedAt).Quality != "unavailable" {
		t.Fatal("failed metadata reported fresh")
	}
	if calls != 2 {
		t.Fatal("authorization issued per-user reads", calls)
	}
}

func TestDomainRoutingLoopSharesOneReadPerNASAndJoinsCancellation(t *testing.T) {
	var calls atomic.Int64
	started := make(chan struct{})
	ended := make(chan struct{})
	one := NAS{RouterID: "lab", Network: "C", LoginDomain: "md-pilot", LoginRouting: &LoginRoutingTarget{LegacyDomain: "md-legacy"}}
	two := one
	two.Network = "D"
	service := &RadiusService{Clients: []NAS{one, two}, ReadLoginRouting: func(ctx context.Context, router string, legacy bool) (LoginRoutingObservation, error) {
		if router != "lab" || !legacy {
			t.Error("wrong router/transition scope")
		}
		if calls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		close(ended)
		return LoginRoutingObservation{}, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(t.Context())
	join := service.startLoginRouting(ctx, func(error) {})
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("independent routing loop did not start")
	}
	cancel()
	join()
	select {
	case <-ended:
	default:
		t.Fatal("routing reader escaped shutdown")
	}
	if calls.Load() != 1 {
		t.Fatal("same NAS was read once per network", calls.Load())
	}
}

func TestNoDomainRoutingCostsNoExtraRouterRead(t *testing.T) {
	service := &RadiusService{Clients: []NAS{{RouterID: "lab", Network: "C"}}, ReadLoginRouting: func(context.Context, string, bool) (LoginRoutingObservation, error) {
		t.Error("direct mode started a routing read")
		return LoginRoutingObservation{}, nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	join := service.startLoginRouting(ctx, func(error) {})
	cancel()
	join()
	if !service.LoginRoutingAllows(service.Clients[0], "legacy@name", time.Now()) {
		t.Fatal("direct mode compatibility changed")
	}
}

func TestPreparedDomainRenewalRequiresVerifiedNamespaceBeforeSending(t *testing.T) {
	for _, safe := range []bool{false, true} {
		t.Run(map[bool]string{true: "isolated", false: "wildcard-fallback"}[safe], func(t *testing.T) {
			store, service, account, grant, start := liveFixture(t)
			at := start.Add(40 * time.Second)
			if err := store.ObserveRouter(t.Context(), observation(service, account, grant, start, at), service.Clients); err != nil {
				t.Fatal(err)
			}
			if _, err := store.PrepareOperation(t.Context(), grant.LeaseID, "renew", at); err != nil {
				t.Fatal(err)
			}
			r, _, target := routingFixture()
			r.Version = "synthetic-7"
			r.ObservedAt = at
			r.Menus["/ip/hotspot/print"][0]["name"] = "hs-lab"
			if !safe {
				r.Menus["/radius/print"] = append(r.Menus["/radius/print"], routeros.Reply{"domain": "", "realm": "", "service": "hotspot", "disabled": "false"})
			}
			service.Clients[0].LoginDomain = "md-pilot"
			service.Clients[0].LoginRouting = &target
			service.ReadLoginRouting = func(context.Context, string, bool) (LoginRoutingObservation, error) {
				return LoginRoutingObservation{Readiness: r, LegacyComplete: true}, nil
			}
			if err := service.RefreshLoginRouting(t.Context(), "lab"); err != nil {
				t.Fatal(err)
			}
			conn, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var packets atomic.Int64
			nas := &radius.PacketServer{SecretSource: radius.StaticSecretSource([]byte(testSecret)), Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
				packets.Add(1)
				_ = w.Write(r.Response(radius.CodeCoAACK))
			})}
			go nas.Serve(conn)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = nas.Shutdown(ctx)
			})
			service.Clients[0].DisconnectPort = conn.LocalAddr().(*net.UDPAddr).Port
			if err := service.ProcessOperations(t.Context(), at); err != nil {
				t.Fatal(err)
			}
			if safe && packets.Load() != 1 || !safe && packets.Load() != 0 {
				t.Fatal("domain renewal ignored namespace guard", safe, packets.Load())
			}
		})
	}
}

func TestUnsafeDomainRouteStillAccountsButDoesNotPrepareRenewal(t *testing.T) {
	store, service, account, grant, start := liveFixture(t)
	at := start.Add(40 * time.Second)
	service.Clients[0].LoginDomain = "md-pilot"
	service.Observe = func(context.Context, string) (RouterObservation, error) {
		return observation(service, account, grant, start, at), nil
	}
	if err := service.Tick(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	operations, err := store.Operations(t.Context(), false)
	if err != nil || len(operations) != 0 {
		t.Fatal("unknown route created a renewal", operations, err)
	}
	snapshot, err := store.Snapshot(t.Context(), account.ID, at)
	if err != nil || snapshot.LifetimeSeconds != 40 || snapshot.LifetimeDownload != 40000 {
		t.Fatal("routing failure stopped accounting", snapshot, err)
	}
}

func TestUnsafeDomainRouteDoesNotKeepExpiredOperationPending(t *testing.T) {
	store, service, account, grant, start := liveFixture(t)
	preparedAt := start.Add(40 * time.Second)
	if err := store.ObserveRouter(t.Context(), observation(service, account, grant, start, preparedAt), service.Clients); err != nil {
		t.Fatal(err)
	}
	operation, err := store.PrepareOperation(t.Context(), grant.LeaseID, "renew", preparedAt)
	if err != nil {
		t.Fatal(err)
	}
	service.Clients[0].LoginDomain = "md-pilot"
	// Missing routing evidence forbids a network write, but must not prevent
	// the bounded operation lifecycle from recording its failed deadline.
	at := preparedAt.Add(16 * time.Second)
	if err = store.ObserveRouter(t.Context(), observation(service, account, grant, start, at), service.Clients); err != nil {
		t.Fatal(err)
	}
	if err = service.ProcessOperations(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Operations(t.Context(), false)
	if err != nil || len(rows) != 1 || rows[0].ID != operation.ID || rows[0].State != "failed" || rows[0].Attempts != 0 || rows[0].Packet != "" || rows[0].Error != "application was not verified before bounded deadline" {
		t.Fatal("unsafe routing hid deadline failure or allowed a packet", rows, err)
	}
}

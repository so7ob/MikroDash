package subscriber

import (
	"errors"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

type connectionTestFixture struct {
	store   *Store
	service *RadiusService
	account Account
	address string
	token   string
	input   TicketRequest
}

func newConnectionTestFixture(t *testing.T) connectionTestFixture {
	t.Helper()
	s, service, account, address := protocolFixture(t, func(service *RadiusService) {
		service.Clients[0].Capabilities = Capabilities{Version: "synthetic-connection", Evidence: "synthetic connection protocol fixture", CounterMode: "input-upload"}
	})
	input := TicketRequest{AccountID: account.ID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:01", ClientAddress: "198.51.100.64"}
	token, err := s.IssueTicket(t.Context(), input, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return connectionTestFixture{s, service, account, address, token, input}
}
func (f connectionTestFixture) query() ConnectionQuery {
	return ConnectionQuery{AccountID: f.account.ID, FlowID: Digest(f.token), ClientAddress: f.input.ClientAddress}
}
func (f connectionTestFixture) status(t *testing.T, at time.Time) ConnectionStatus {
	t.Helper()
	v, err := f.store.ConnectionStatusFor(t.Context(), f.query(), f.service.Clients, at)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version == "" || v.Reasons == nil {
		t.Fatal("unversioned or nullable connection payload", v)
	}
	return v
}

func TestConnectionStatusRequiresOwnFreshObservedSession(t *testing.T) {
	f := newConnectionTestFixture(t)
	if v := f.status(t, time.Now()); v.Stage != "requested" || v.LeaseID != "" {
		t.Fatal("ticket creation opened Internet", v)
	}
	packet := access(f.token)
	reply := exchange(t, packet, f.address)
	if reply.Code != radius.CodeAccessAccept {
		t.Fatal(reply.Code)
	}
	lease := rfc2865.Class_GetString(reply)
	if v := f.status(t, time.Now()); v.Stage != "authorized" || v.LeaseID != lease {
		t.Fatal("RADIUS authorization and router confirmation were collapsed", v)
	}
	var created int64
	if err := f.store.sql.QueryRow(`SELECT created_at FROM subscriber_leases WHERE id=?`, lease).Scan(&created); err != nil {
		t.Fatal(err)
	}
	start := time.Unix(created, 0)
	event := Accounting{LeaseID: lease, RouterID: "lab", Network: "C", Device: f.input.Device, SessionID: "synthetic-connection-session", Status: "start", At: start}
	if err := f.store.Account(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if v := f.status(t, start); v.Stage != "authorized" {
		t.Fatal("Start alone bypassed fresh router confirmation", v)
	}
	// Subsequent readings use a virtual clock, not a long hardware wait.
	at := start.Add(2 * time.Second)
	view := RouterObservation{RouterID: "lab", Version: "synthetic-connection", At: at, Complete: true, Sessions: []ObservedSession{{ID: "*64", Server: "hs-lab", Username: f.account.Username, Device: f.input.Device, Address: f.input.ClientAddress, Seconds: 2, CountersKnown: true, TimeoutKnown: true, TimeLeft: 58}}}
	if err := f.store.ObserveRouter(t.Context(), view, f.service.Clients); err != nil {
		t.Fatal(err)
	}
	connected := f.status(t, at)
	if connected.Stage != "connected" || connected.LeaseID != lease || !connected.ObservedAt.Equal(at) {
		t.Fatal("owned observed session was not confirmed", connected)
	}
	stale := f.status(t, at.Add(6*time.Second))
	if stale.Stage != "unconfirmed" || stale.Version == connected.Version || !stale.ObservedAt.Equal(connected.ObservedAt) {
		t.Fatal("stale data became current/disconnected or lost its trusted timestamp", stale)
	}
	if v := f.status(t, at.Add(-2*time.Second)); v.Stage == "connected" {
		t.Fatal("future observation confirmed Internet", v)
	}
	changed := append([]NAS{}, f.service.Clients...)
	changed[0].Capabilities.Version = "synthetic-other-version"
	if v, err := f.store.ConnectionStatusFor(t.Context(), f.query(), changed, at); err != nil || v.Stage == "connected" {
		t.Fatal("unqualified firmware observation confirmed Internet", v, err)
	}
	for _, q := range []ConnectionQuery{{AccountID: "foreign-account", FlowID: Digest(f.token), ClientAddress: f.input.ClientAddress}, {AccountID: f.account.ID, FlowID: Digest(f.token), ClientAddress: "198.51.100.65"}, {AccountID: f.account.ID, FlowID: "not-a-flow", ClientAddress: f.input.ClientAddress}} {
		if _, err := f.store.ConnectionStatusFor(t.Context(), q, f.service.Clients, at); !errors.Is(err, ErrDenied) {
			t.Fatal("unowned flow accepted", err)
		}
	}
	// A credential's 30-second TTL is independent of an established connection.
	view.At = start.Add(31 * time.Second)
	view.Sessions[0].Seconds = 31
	view.Sessions[0].TimeLeft = 29
	if err := f.store.ObserveRouter(t.Context(), view, f.service.Clients); err != nil {
		t.Fatal(err)
	}
	if v := f.status(t, view.At); v.Stage != "connected" {
		t.Fatal("expired credential ended a confirmed Internet session", v)
	}
	event.Status = "stop"
	event.At = start.Add(32 * time.Second)
	event.Seconds = 32
	if err := f.store.Account(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if v := f.status(t, event.At); v.Stage != "disconnected" {
		t.Fatal("trusted final Stop left a false connected state", v)
	}
	final, err := f.store.Snapshot(t.Context(), f.account.ID, event.At)
	if err != nil || final.LifetimeSeconds != 32 || final.Decision.Limits[0].Remaining != 3568 {
		t.Fatal("connection tracking changed commercial usage", final, err)
	}
}

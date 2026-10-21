package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"mikrodash/internal/subscriber"
)

func TestPortalConnectionSocketChecksOwnerPeerAndOriginBeforeUpgrade(t *testing.T) {
	s, account, cookie, _ := portalFixture(t)
	ledger := s.subscribers.radius.Store
	foreign, _, err := ledger.CreateAccount(t.Context(), "synthetic-admin", "synthetic-foreign-socket", "Synthetic foreign owner", "synthetic-password", "socket ownership fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	foreignToken, _ := s.subscribers.sessions.New(foreign.ID)
	own, err := ledger.IssueTicket(t.Context(), subscriber.TicketRequest{AccountID: account.ID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:64", ClientAddress: "127.0.0.1"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	wrongPeer, err := ledger.IssueTicket(t.Context(), subscriber.TicketRequest{AccountID: account.ID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:65", ClientAddress: "198.51.100.65"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewTLSServer(s.subscriberPortal(t.TempDir()))
	defer edge.Close()
	s.subscribers.cfg.PortalOrigin = edge.URL
	for _, tc := range []struct {
		name, flow, origin, token string
		code                      int
	}{
		{"anonymous", subscriber.Digest(own), edge.URL, "", 401},
		{"foreign-account", subscriber.Digest(own), edge.URL, foreignToken, 404},
		{"foreign-peer", subscriber.Digest(wrongPeer), edge.URL, cookie.Value, 404},
		{"invalid-reference", "not-a-flow", edge.URL, cookie.Value, 404},
		{"foreign-origin", subscriber.Digest(own), "https://foreign.example.test", cookie.Value, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			headers := http.Header{"Origin": {tc.origin}, "Cookie": {cookie.Name + "=" + tc.token}}
			ws, response, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(edge.URL, "https")+"/ws?flow="+tc.flow, &websocket.DialOptions{HTTPClient: edge.Client(), HTTPHeader: headers})
			if ws != nil {
				ws.CloseNow()
			}
			if err == nil || response == nil || response.StatusCode != tc.code {
				t.Fatal("unowned connection socket upgraded", tc.code, response, err)
			}
		})
	}
}

func TestPortalConnectionSocketDeliversOwnedTransitionsAndRevokes(t *testing.T) {
	s, account, cookie, _ := portalFixture(t)
	ledger := s.subscribers.radius.Store
	nas := []subscriber.NAS{{RouterID: "lab", Network: "C", CalledStationID: "hs-lab", Capabilities: subscriber.Capabilities{Version: "synthetic-socket", Evidence: "synthetic TLS socket fixture", CounterMode: "input-upload"}}}
	s.subscribers.cfg.Clients = nas
	token, err := ledger.IssueTicket(t.Context(), subscriber.TicketRequest{AccountID: account.ID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:64", ClientAddress: "127.0.0.1"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewTLSServer(s.subscriberPortal(t.TempDir()))
	defer edge.Close()
	s.subscribers.cfg.PortalOrigin = edge.URL
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	headers := http.Header{"Origin": {edge.URL}, "Cookie": {cookie.String()}}
	ws, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(edge.URL, "https")+"/ws?flow="+subscriber.Digest(token), &websocket.DialOptions{HTTPClient: edge.Client(), HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	readConnection := func() subscriber.ConnectionStatus {
		t.Helper()
		readCtx, done := context.WithTimeout(ctx, 3*time.Second)
		defer done()
		for {
			_, frame, err := ws.Read(readCtx)
			if err != nil {
				t.Fatal("owned connection event missing", err)
			}
			var envelope struct {
				Event string          `json:"event"`
				Data  json.RawMessage `json:"data"`
			}
			if err = json.Unmarshal(frame, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Event == "subscriber:snapshot" {
				var snapshot subscriber.Snapshot
				if err = json.Unmarshal(envelope.Data, &snapshot); err != nil || snapshot.Subscription.AccountID != account.ID {
					t.Fatal("unowned account frame", err)
				}
				continue
			}
			if envelope.Event != "subscriber:connection" {
				t.Fatal("unexpected portal event", envelope.Event)
			}
			var status subscriber.ConnectionStatus
			if err = json.Unmarshal(envelope.Data, &status); err != nil || status.FlowID != subscriber.Digest(token) || status.Reasons == nil || status.Version == "" {
				t.Fatal("invalid owned connection payload", status, err)
			}
			return status
		}
	}
	if got := readConnection(); got.Stage != "requested" {
		t.Fatal("ticket creation claimed Internet", got)
	}
	at := time.Now().Truncate(time.Second)
	binding := subscriber.TicketBinding{RouterID: "lab", Network: "C", Device: "02:00:00:00:00:64", Username: account.Username}
	if _, err = ledger.RedeemTicket(ctx, token, binding, "synthetic-socket-grant", at); err != nil {
		t.Fatal(err)
	}
	grant, err := ledger.Reserve(ctx, account.ID, "lab", "C", binding.Device, "synthetic-socket-grant", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.RecordTicketOutcome(ctx, token, binding, "synthetic-socket-grant", subscriber.TicketOutcome{LeaseID: grant.LeaseID}, at); err != nil {
		t.Fatal(err)
	}
	got := readConnection()
	if got.LeaseID != grant.LeaseID || (got.Stage != "authorized" && got.Stage != "unconfirmed") {
		t.Fatal("authorization was not delivered independently of confirmation", got)
	}
	observed := time.Now().Truncate(time.Second)
	view := subscriber.RouterObservation{RouterID: "lab", Version: "synthetic-socket", At: observed, Complete: true, Sessions: []subscriber.ObservedSession{{ID: "*64", Server: "hs-lab", Username: account.Username, Device: binding.Device, Address: "127.0.0.1", Seconds: subscriber.Quantity(observed.Sub(at) / time.Second), CountersKnown: true, TimeoutKnown: true, TimeLeft: 55}}}
	if err = ledger.ObserveRouter(ctx, view, nas); err != nil {
		t.Fatal(err)
	}
	if got = readConnection(); got.Stage != "connected" || !got.ObservedAt.Equal(observed) {
		t.Fatal("fresh owned observation missing from socket", got)
	}
	s.subscribers.sessions.Delete(cookie.Value)
	for {
		_, _, err = ws.Read(ctx)
		if err != nil {
			break
		}
		t.Fatal("socket sent data after portal revocation")
	}
}

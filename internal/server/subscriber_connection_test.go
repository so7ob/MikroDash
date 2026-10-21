package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/subscriber"
)

func TestPortalConnectionConfirmsOnlyTheOwnedRequestingDevice(t *testing.T) {
	s, account, cookie, _ := portalFixture(t)
	ledger := s.subscribers.radius.Store
	nas := []subscriber.NAS{{RouterID: "lab", Network: "C", CalledStationID: "hs-lab", Capabilities: subscriber.Capabilities{Version: "synthetic-lab", Evidence: "synthetic connection observation", CounterMode: "input-upload"}}}
	s.subscribers.cfg.Clients = nas
	s.subscribers.radius.Clients = nas
	token, err := ledger.IssueTicket(t.Context(), subscriber.TicketRequest{AccountID: account.ID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:64", ClientAddress: "192.0.2.1"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Truncate(time.Second)
	other, err := ledger.Reserve(t.Context(), account.ID, "lab", "C", "02:00:00:00:00:65", "synthetic-other-device", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.Account(t.Context(), subscriber.Accounting{LeaseID: other.LeaseID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:65", SessionID: "other-device", Status: "start", At: at}); err != nil {
		t.Fatal(err)
	}
	view := subscriber.RouterObservation{RouterID: "lab", Version: "synthetic-lab", At: at, Complete: true, Sessions: []subscriber.ObservedSession{{ID: "*1", Server: "hs-lab", Username: account.Username, Device: "02:00:00:00:00:65", Address: "198.51.100.65", CountersKnown: true, TimeoutKnown: true, TimeLeft: 60}}}
	if err = ledger.ObserveRouter(t.Context(), view, nas); err != nil {
		t.Fatal(err)
	}
	before, err := ledger.Snapshot(t.Context(), account.ID, at)
	if err != nil || len(before.Sessions) != 1 || before.Sessions[0].ConnectionState != "connected" {
		t.Fatal("negative-control device is not actually observed online", before, err)
	}
	handler := s.subscriberPortal(t.TempDir())
	r := httptest.NewRequest("GET", "https://portal.example.test/api/connection?flow="+subscriber.Digest(token), nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var status struct {
		Stage   string `json:"stage"`
		Network string `json:"network"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &status); w.Code != 200 || err != nil || status.Stage != "requested" || status.Network != "C" {
		t.Fatal("another device became confirmation for this request, or own flow is unavailable", w.Code, w.Body.String(), err)
	}
	for _, test := range []struct {
		name, peer string
		auth       bool
	}{{"anonymous", "192.0.2.1:1234", false}, {"other-device", "198.51.100.99:1234", true}} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://portal.example.test/api/connection?flow="+subscriber.Digest(token), nil)
			r.RemoteAddr = test.peer
			if test.auth {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			expected := 404
			if !test.auth {
				expected = 401
			}
			if w.Code != expected || strings.Contains(w.Body.String(), "02:00:00:00:00:64") {
				t.Fatal("unowned flow access", w.Code, w.Body.String())
			}
		})
	}
}

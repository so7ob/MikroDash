package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/subscriber"
)

func TestSubscriberAdministrationWithoutNetworkRuntime(t *testing.T) {
	s, mux, _ := reconciliationFixture(t)
	s.subscribers = nil
	if err := s.StartSubscribers("", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Host = "admin.example.test"
		r.Header.Set("Origin", "https://admin.example.test")
		r.Header.Set("X-MikroDash-Request", "subscriber-admin")
		r.Header.Set("Cookie", "mikrodash_sid=synthetic-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	get := request("GET", "/api/subscribers", nil)
	if get.Code != 200 {
		t.Fatalf("setup requires a running NAS: %d %s", get.Code, get.Body.String())
	}
	var list struct {
		Mode     string               `json:"mode"`
		Accounts []subscriber.Account `json:"accounts"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &list); err != nil || list.Mode != "disabled" {
		t.Fatal(list, err)
	}
	created := request("POST", "/api/subscribers", map[string]any{"action": "create", "username": "synthetic-offline", "name": "Synthetic Unicode: اختبار", "password": "synthetic-password", "reason": "Synthetic offline setup"})
	if created.Code != 200 {
		t.Fatal(created.Code, created.Body.String())
	}
	var account struct {
		Account subscriber.Account `json:"account"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &account); err != nil || account.Account.Name != "Synthetic Unicode: اختبار" {
		t.Fatal(account, err)
	}
	plan := subscriber.Plan{ID: "synthetic-offline", Name: "Synthetic trial plan", Version: 1, ValidSeconds: 86400, Start: "first-login", MaxSessions: 1, Networks: []string{"C"}, Down: 8000000, Up: 2000000, Speeds: []subscriber.Speed{{ID: "economy", Name: "Economy", Down: 2000000, Up: 1000000}, {ID: "fast", Name: "Fast", Down: 8000000, Up: 2000000}}, Policies: []subscriber.Policy{{ID: "synthetic-concurrent", Limits: []subscriber.Limit{{ID: "time", Kind: "seconds", State: "limited", Amount: 3600, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: time.Now().Add(-time.Hour)}}, {ID: "data", Kind: "download", State: "limited", Amount: 2147483648, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: time.Now().Add(-time.Hour)}}}}}}
	for _, body := range []any{
		map[string]any{"action": "plan", "plan": plan, "reason": "Synthetic offline plan"},
		map[string]any{"action": "assign", "accountId": account.Account.ID, "planId": plan.ID, "version": 1, "reason": "Synthetic offline assignment"},
	} {
		if w := request("POST", "/api/subscribers", body); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	snapshot := request("GET", "/api/subscribers/snapshot?account="+account.Account.ID, nil)
	if snapshot.Code != 200 {
		t.Fatal(snapshot.Code, snapshot.Body.String())
	}
	var view subscriber.Snapshot
	if err := json.Unmarshal(snapshot.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Subscription.Status != "pending" || view.LifetimeSeconds != 0 || len(view.Decision.Limits) != 2 || view.Decision.Limits[0].Remaining != 3600 || view.Decision.Limits[1].Remaining != 2147483648 || !view.Subscription.ActivatedAt.IsZero() {
		t.Fatal(view)
	}
	for _, status := range []string{"suspended", "active"} {
		if w := request("POST", "/api/subscribers", map[string]any{"action": "status", "accountId": account.Account.ID, "status": status, "reason": "Synthetic offline status"}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request("GET", "/api/subscribers/reconciliation?account="+account.Account.ID, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"gaps":[]`) {
		t.Fatal(w.Code, w.Body.String())
	}
	// No runtime, listener, background connection or authentication was enabled.
	if s.subscribers != nil {
		t.Fatal("administration started the network runtime")
	}
	accounts, err := s.auditDB.Subscribers().Accounts(t.Context())
	if err != nil || len(accounts) != 2 {
		t.Fatal(accounts, err)
	}
	preserved, err := s.auditDB.Subscribers().Snapshot(t.Context(), account.Account.ID, time.Now())
	if err != nil || preserved.Subscription.ID != view.Subscription.ID || preserved.LifetimeDownload != 0 {
		t.Fatal(preserved, err)
	}
}

func TestSubscriberOfflineAdministrationRetainsSecurityAndStorageGuards(t *testing.T) {
	s, mux, _ := reconciliationFixture(t)
	s.subscribers = nil
	for _, tc := range []struct {
		cookie, origin, header string
		code                   int
	}{
		{"", "https://admin.example.test", "subscriber-admin", 401},
		{"mikrodash_sid=synthetic-token", "https://evil.example.test", "subscriber-admin", 403},
		{"mikrodash_sid=synthetic-token", "", "subscriber-admin", 403},
		{"mikrodash_sid=synthetic-token", "https://admin.example.test", "", 403},
	} {
		r := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(`{"action":"create"}`))
		r.Host = "admin.example.test"
		r.Header.Set("Cookie", tc.cookie)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-MikroDash-Request", tc.header)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "modern", Username: "unprivileged"})
	r := httptest.NewRequest("GET", "/api/subscribers", nil)
	r.Header.Set("Cookie", "mikrodash_sid=synthetic-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "modern", Username: "synthetic-admin"})
	s.auditDB = nil
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "Subscriber ledger unavailable") {
		t.Fatal(w.Code, w.Body.String())
	}
}

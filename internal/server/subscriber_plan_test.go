package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/subscriber"
)

// Exercise the protected HTTP boundary and actual SQLite authority: the browser
// harness separately exercises the editor, but its mock cannot prove persistence.
func TestSubscriberPlanEditorPersistsConcurrentLimitsAndImmutableAssignment(t *testing.T) {
	s, mux, account := reconciliationFixture(t)
	s.subscribers = nil
	request := func(body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(string(raw)))
		r.Host = "admin.example.test"
		r.Header.Set("Origin", "https://admin.example.test")
		r.Header.Set("X-MikroDash-Request", "subscriber-admin")
		r.Header.Set("Cookie", "mikrodash_sid=synthetic-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	period := subscriber.Period{Kind: "monthly", Zone: "Asia/Aden", Anchor: time.Date(2026, 1, 31, 0, 25, 0, 0, time.UTC)}
	p := subscriber.Plan{ID: "synthetic-editor", Version: 1, Name: "باقة synthetic", ValidSeconds: 2592000, Start: "first-login", MaxSessions: 2, Networks: []string{"C"}, Down: 8000000, Up: 2000000,
		Speeds: []subscriber.Speed{{ID: "fast", Name: "Fast", Down: 8000000, Up: 2000000}},
		Policies: []subscriber.Policy{{ID: "standard", Limits: []subscriber.Limit{
			{ID: "limit-1", Kind: "seconds", State: "limited", Amount: 3600, Period: period, Action: "disconnect"},
			{ID: "limit-3", Kind: "total", State: "limited", Amount: 9007199254740993, Period: period, Action: "disconnect"},
			{ID: "limit-4", Kind: "download", State: "limited", Amount: 0, Period: period, Action: "throttle", Down: 250000, Up: 125000},
		}}},
	}
	save := func(plan subscriber.Plan) *httptest.ResponseRecorder {
		return request(map[string]any{"action": "plan", "plan": plan, "reason": "Synthetic editor review"})
	}
	if w := save(p); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(map[string]any{"action": "assign", "accountId": account, "planId": p.ID, "version": 1, "reason": "Synthetic assignment"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Publishing a later draft never rewrites a subscription's historical plan.
	p.Version = 2
	p.Policies[0].Limits[1].Amount = 7
	p.Name = "New draft"
	if w := save(p); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	p.Version = 1
	if w := save(p); w.Code != 400 {
		t.Fatal("existing version overwritten", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/subscribers/snapshot?account="+account, nil)
	r.Header.Set("Cookie", "mikrodash_sid=synthetic-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var v subscriber.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	old := v.Subscription.Plan
	if old.Version != 1 || old.Name != "باقة synthetic" || v.Subscription.Status != "pending" || len(old.Policies) != 1 || len(old.Policies[0].Limits) != 3 {
		t.Fatal(v.Subscription)
	}
	limits := old.Policies[0].Limits
	if limits[0].ID != "limit-1" || limits[1].ID != "limit-3" || limits[1].Amount != 9007199254740993 || limits[1].Period != period {
		t.Fatal("persisted identity/quantity/calendar changed", limits)
	}
	if !v.Decision.Allowed || v.Decision.Down != 250000 || v.Decision.Up != 125000 || len(v.Decision.Limits) != 3 || v.LifetimeDownload != 0 || v.LifetimeSeconds != 0 {
		t.Fatal("concurrent FUP/quotas or usage changed", v)
	}
	if s.subscribers != nil {
		t.Fatal("plan setup activated network runtime")
	}
}

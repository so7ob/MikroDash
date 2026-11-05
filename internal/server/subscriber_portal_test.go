package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/db"
	"mikrodash/internal/hub"
	"mikrodash/internal/subscriber"
)

func portalFixture(t *testing.T) (*Server, subscriber.Account, *http.Cookie, string) {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ledger := d.Subscribers()
	a, _, err := ledger.CreateAccount(context.Background(), "synthetic-admin", "synthetic-account", "Synthetic fixture", "synthetic-password", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	p := subscriber.Plan{ID: "test", Version: 1, Name: "Synthetic test", ValidSeconds: 3600, Start: "assigned", MaxSessions: 1, Networks: []string{"C"}, Down: 8000000, Up: 2000000, Speeds: []subscriber.Speed{{ID: "economy", Name: "Economy", Down: 2000000, Up: 1000000}, {ID: "fast", Name: "Fast", Down: 8000000, Up: 2000000}}, Policies: []subscriber.Policy{{ID: "time", Limits: []subscriber.Limit{{ID: "hour", Kind: "seconds", State: "limited", Amount: 3600, Action: "disconnect", Period: subscriber.Period{Kind: "daily", Zone: "Asia/Aden", Anchor: time.Now().Add(-time.Hour)}}}}}}
	if err = ledger.SavePlan(context.Background(), "synthetic-admin", "test", p); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.Assign(context.Background(), "synthetic-admin", a.ID, p.ID, 1, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	s := &Server{auditDB: d, subscribers: &subscriberRuntime{cfg: subscriber.Config{Mode: "shadow", PortalOrigin: "https://portal.example.test"}, radius: &subscriber.RadiusService{Store: ledger, Mode: "shadow"}, hub: hub.New()}}
	r := httptest.NewRequest("POST", "https://portal.example.test/api/login", strings.NewReader(`{"username":"synthetic-account","password":"synthetic-password"}`))
	r.Header.Set("Origin", "https://portal.example.test")
	w := httptest.NewRecorder()
	s.subscriberPortal(t.TempDir()).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" {
		t.Fatal("unsafe cookie", cookies)
	}
	var reply struct {
		CSRF string `json:"csrf"`
	}
	json.Unmarshal(w.Body.Bytes(), &reply)
	return s, a, cookies[0], reply.CSRF
}
func TestPortalDoesNotExposeAdminRoutes(t *testing.T) {
	s, _, cookie, _ := portalFixture(t)
	handler := s.subscriberPortal(t.TempDir())
	for _, path := range []string{"/api/routers", "/api/users", "/api/auth/status", "/api/subscribers", "/api/subscribers/reconciliation", "/app.js", "/settings"} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
}
func TestPortalCSRFAndOwnership(t *testing.T) {
	s, a, cookie, csrf := portalFixture(t)
	handler := s.subscriberPortal(t.TempDir())
	for _, tc := range []struct {
		origin, csrf, body string
		code               int
	}{{"https://evil.example.test", csrf, `{"profileId":"fast"}`, 403}, {"https://portal.example.test", "wrong", `{"profileId":"fast"}`, 403}, {"https://portal.example.test", csrf, `{"profileId":"unauthorized"}`, 400}, {"https://portal.example.test", csrf, `{"profileId":"fast","accountId":"someone-else"}`, 400}, {"https://portal.example.test", csrf, `{"profileId":"fast"}`, 200}} {
		r := httptest.NewRequest("POST", "/api/speed", strings.NewReader(tc.body))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-CSRF-Token", tc.csrf)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/api/account?account=someone-else", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var snap subscriber.Snapshot
	json.Unmarshal(w.Body.Bytes(), &snap)
	if snap.Subscription.AccountID != a.ID {
		t.Fatal("cross-account read")
	}
	r = httptest.NewRequest("POST", "/api/disconnect", bytes.NewBufferString(`{"leaseId":"foreign","confirm":true}`))
	r.Header.Set("Origin", "https://portal.example.test")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestPortalSessionIsIndependentAndLogoutRevokes(t *testing.T) {
	s, _, cookie, csrf := portalFixture(t)
	handler := s.subscriberPortal(t.TempDir())
	r := httptest.NewRequest("GET", "/api/account", nil)
	r.AddCookie(&http.Cookie{Name: "mikrodash_sid", Value: cookie.Value})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("admin cookie accepted")
	}
	r = httptest.NewRequest("POST", "/api/logout", strings.NewReader(`{}`))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://portal.example.test")
	r.Header.Set("X-CSRF-Token", csrf)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("GET", "/api/account", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("logged out cookie still valid")
	}
}
func TestShadowCannotIssueHotSpotTicket(t *testing.T) {
	s, _, cookie, csrf := portalFixture(t)
	r := httptest.NewRequest("POST", "/api/connect", strings.NewReader(`{"network":"C"}`))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://portal.example.test")
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	s.subscriberPortal(t.TempDir()).ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
}

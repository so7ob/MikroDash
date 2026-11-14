package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"mikrodash/internal/subscriber"
)

func TestSubscriberCredentialAPIRequiresReviewAuthorityAndExactRevision(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	s.subscribers = nil
	request := func(body string, cookie, origin, header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
		r.Host = "admin.example.test"
		r.Header.Set("Cookie", cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-MikroDash-Request", header)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	valid := `{"action":"credential","accountId":"` + id + `","password":"synthetic-reset-password","reason":"Synthetic reviewed reset","expectedCredentialRevision":"0","confirmCredentialReset":true}`
	for _, tc := range []struct {
		cookie, origin, header string
		code                   int
	}{{"", "https://admin.example.test", "subscriber-admin", 401}, {"mikrodash_sid=synthetic-token", "https://foreign.example.test", "subscriber-admin", 403}, {"mikrodash_sid=synthetic-token", "https://admin.example.test", "", 403}} {
		w := request(valid, tc.cookie, tc.origin, tc.header)
		if w.Code != tc.code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, body := range []string{strings.Replace(valid, `"expectedCredentialRevision":"0"`, `"expectedCredentialRevision":0`, 1), strings.Replace(valid, `"expectedCredentialRevision":"0",`, "", 1), strings.Replace(valid, `"confirmCredentialReset":true`, `"confirmCredentialReset":false`, 1), strings.Replace(valid, "synthetic-reset-password", "short", 1)} {
		if w := request(body, "mikrodash_sid=synthetic-token", "https://admin.example.test", "subscriber-admin"); w.Code != 400 {
			t.Fatal("invalid reset accepted", w.Code)
		}
	}
	oldAuth := s.auth
	s.auth = authFor("viewer-token", &Session{AuthMode: "modern", Username: "synthetic-viewer"})
	if w := request(valid, "mikrodash_sid=viewer-token", "https://admin.example.test", "subscriber-admin"); w.Code != 403 {
		t.Fatal("viewer reset credential", w.Code)
	}
	s.auth = oldAuth
	w := request(valid, "mikrodash_sid=synthetic-token", "https://admin.example.test", "subscriber-admin")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct{ Account subscriber.Account }
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Account.ID != id || result.Account.CredentialRevision != 1 {
		t.Fatal("reset reply lost exact account revision")
	}
	for _, secret := range []string{"synthetic-reset-password", "password_hash", "salt", "voucher_hash"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("reset response leaked credential material")
		}
	}
	if w = request(valid, "mikrodash_sid=synthetic-token", "https://admin.example.test", "subscriber-admin"); w.Code != 409 {
		t.Fatal("stale reset overwrote current credential", w.Code, w.Body.String())
	}
	if _, err := s.auditDB.Subscribers().Authenticate(t.Context(), "synthetic-account", "synthetic-reset-password"); err != nil {
		t.Fatal("new password did not authenticate", err)
	}
}

type delayedLoginWriter struct {
	*httptest.ResponseRecorder
	ready   chan struct{}
	release chan struct{}
}

func (w *delayedLoginWriter) Write(body []byte) (int, error) {
	close(w.ready)
	<-w.release
	return w.ResponseRecorder.Write(body)
}

func TestCredentialResetRevokesOldHTTPAndLateLoginResponse(t *testing.T) {
	s, a, oldCookie, oldCSRF := portalFixture(t)
	handler := s.subscriberPortal(t.TempDir())
	pending := &delayedLoginWriter{httptest.NewRecorder(), make(chan struct{}), make(chan struct{})}
	done := make(chan struct{})
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"synthetic-account","password":"synthetic-password"}`))
	r.Header.Set("Origin", s.subscribers.cfg.PortalOrigin)
	go func() { handler.ServeHTTP(pending, r); close(done) }()
	select {
	case <-pending.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("login did not reach response boundary")
	}
	_, err := s.subscribers.radius.Store.ResetCredential(t.Context(), "synthetic-admin", a.ID, "synthetic-current-password", "Synthetic reset before pending login response", 0)
	close(pending.release)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	// Direct store reset deliberately does not evict the registry. Durable checks
	// must revoke tokens even if an API callback was lost or another process reset.
	cookies := pending.Result().Cookies()
	if len(cookies) != 1 || pending.Code != 200 {
		t.Fatal("missing pending-login fixture")
	}
	for _, cookie := range []*http.Cookie{oldCookie, cookies[0]} {
		for _, method := range []string{"GET", "POST"} {
			path := "/api/session"
			body := ""
			if method == "POST" {
				path = "/api/speed"
				body = `{"profileId":"fast"}`
			}
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.AddCookie(cookie)
			req.Header.Set("Origin", s.subscribers.cfg.PortalOrigin)
			req.Header.Set("X-CSRF-Token", oldCSRF)
			if method == "POST" && cookie == cookies[0] {
				var value struct{ CSRF string }
				json.Unmarshal(pending.Body.Bytes(), &value)
				req.Header.Set("X-CSRF-Token", value.CSRF)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 401 {
				t.Fatal("revoked HTTP proof remained valid", method, w.Code)
			}
		}
	}
	login := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"synthetic-account","password":"synthetic-current-password"}`))
	login.Header.Set("Origin", s.subscribers.cfg.PortalOrigin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, login)
	if w.Code != 200 {
		t.Fatal("current credential refused", w.Code)
	}
	req := httptest.NewRequest("GET", "/api/session", nil)
	req.AddCookie(w.Result().Cookies()[0])
	current := httptest.NewRecorder()
	handler.ServeHTTP(current, req)
	if current.Code != 200 {
		t.Fatal("fresh session was invalid", current.Code)
	}
}

func TestCredentialResetRevokesOpenPortalSocketFromDurableEpoch(t *testing.T) {
	s, a, cookie, _ := portalFixture(t)
	edge := httptest.NewTLSServer(s.subscriberPortal(t.TempDir()))
	defer edge.Close()
	s.subscribers.cfg.PortalOrigin = edge.URL
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(edge.URL, "https")+"/ws", &websocket.DialOptions{HTTPClient: edge.Client(), HTTPHeader: http.Header{"Origin": {edge.URL}, "Cookie": {cookie.String()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if _, _, err = ws.Read(ctx); err != nil {
		t.Fatal("missing initial owned snapshot", err)
	}
	if _, err = s.subscribers.radius.Store.ResetCredential(t.Context(), "synthetic-admin", a.ID, "synthetic-socket-new-password", "Synthetic socket revocation", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err = ws.Read(ctx); err == nil {
		t.Fatal("revoked socket received another snapshot")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("revoked socket stayed open until deadline")
	}
}

func TestPortalEpochDatabaseFailureDoesNotUseCachedSession(t *testing.T) {
	s, _, cookie, _ := portalFixture(t)
	handler := s.subscriberPortal(t.TempDir())
	s.auditDB.Close()
	r := httptest.NewRequest("GET", "/api/session", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "sql") {
		t.Fatal("database failure did not fail closed safely", w.Code, w.Body.String())
	}
}

func TestSubscriberCredentialStorageFailureRequiresNewReview(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	writer, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: s.auditDB.Path()}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err = writer.Exec(`CREATE TRIGGER synthetic_reset_failure BEFORE INSERT ON subscriber_audit WHEN NEW.action='account.credential.reset' BEGIN SELECT RAISE(FAIL,'synthetic private storage path'); END;`); err != nil {
		t.Fatal(err)
	}
	body := `{"action":"credential","accountId":"` + id + `","password":"synthetic-failed-reset","reason":"Synthetic refused storage reset","expectedCredentialRevision":"0","confirmCredentialReset":true}`
	r := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(body))
	r.Host = "admin.example.test"
	r.Header.Set("Cookie", "mikrodash_sid=synthetic-token")
	r.Header.Set("Origin", "https://admin.example.test")
	r.Header.Set("X-MikroDash-Request", "subscriber-admin")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "synthetic private") || !strings.Contains(w.Body.String(), "Refresh") {
		t.Fatal("storage error did not fence an uncertain reset safely", w.Code, w.Body.String())
	}
	current, err := s.auditDB.Subscribers().AccountByID(t.Context(), id)
	if err != nil || current.CredentialRevision != 0 {
		t.Fatal("failed reset changed epoch", err)
	}
	if _, err = s.auditDB.Subscribers().Authenticate(t.Context(), "synthetic-account", "synthetic-password"); err != nil {
		t.Fatal("failed reset changed credential", err)
	}
}

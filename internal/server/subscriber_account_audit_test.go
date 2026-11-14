package server

import (
	"database/sql"
	"encoding/json"
	"mikrodash/internal/subscriber"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSubscriberAccountAuditAuthorityFiltersAndDisabledRuntime(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	s.subscribers = nil
	request := func(query, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/subscribers/audit"+query, nil)
		r.Header.Set("Cookie", cookie)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	const cookie = "mikrodash_sid=synthetic-token"
	query := "?account=" + id
	if w := request(query, ""); w.Code != 401 {
		t.Fatal("unauthenticated audit", w.Code)
	}
	if _, err := s.auditDB.Subscribers().ResetCredential(t.Context(), "synthetic-admin-id", id, "synthetic-private-new-password", "Synthetic reset chronology", 0); err != nil {
		t.Fatal(err)
	}
	w := request(query, cookie)
	var page subscriber.AccountAuditPage
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.AccountID != id || len(page.Events) != 3 {
		t.Fatal("protected offline audit unavailable", w.Code, w.Body.String())
	}
	if page.Events[0].Action != "account.credential.reset" || page.Events[0].Actor != "synthetic-admin-id" {
		t.Fatal("audit lost actor/action")
	}
	if strings.Contains(w.Body.String(), "synthetic-private-new-password") || strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("audit leaked credentials")
	}
	for _, suffix := range []string{"&before=%zz", "&before=0", "&before=-1", "&before=01", "&before=+1", "&before=1.5", "&before=9223372036854775808", "&before=", "&before=1&before=2", "&account=other", "&category=unknown", "&category=profile&category=status", "&from=2026-11-15&until=2026-11-14", "&from=2026-02-30", "&from=2026-11-14T00:00:00Z", "&until=", "&category=", "&scope=all"} {
		if w := request(query+suffix, cookie); w.Code != 400 {
			t.Fatal("invalid query accepted", suffix, w.Code)
		}
	}
	if w := request("", cookie); w.Code != 400 {
		t.Fatal("missing account accepted", w.Code)
	}
	if w := request("?account=synthetic-missing", cookie); w.Code != 404 {
		t.Fatal("missing account was not explicit", w.Code)
	}
	if w := request(query+"&before=9223372036854775807", cookie); w.Code != 200 {
		t.Fatal("large cursor refused", w.Code)
	}
	for _, category := range []string{"profile", "status", "credentials", "subscriptions", "speed"} {
		if w := request(query+"&category="+category, cookie); w.Code != 200 {
			t.Fatal("valid category refused", category, w.Code)
		}
	}
	if w := request(query+"&from=1999-01-01&until=1999-01-02", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"events":[]`) {
		t.Fatal("empty dated audit is not clear", w.Code, w.Body.String())
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "legacy", Username: "synthetic-admin"})
	if w := request(query, cookie); w.Code != 403 {
		t.Fatal("legacy audit access", w.Code)
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "modern", Username: "unprivileged"})
	if w := request(query, cookie); w.Code != 403 {
		t.Fatal("viewer audit access", w.Code)
	}
}

func TestSubscriberAccountAuditPersistsScopeAndSafeStorageErrors(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	ledger := s.auditDB.Subscribers()
	other, _, err := ledger.CreateAccount(t.Context(), "synthetic-admin-id", "synthetic-foreign-audit", "Synthetic other", "synthetic-other-password", "Synthetic private foreign creation", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.Assign(t.Context(), "synthetic-admin-id", other.ID, "test", 1, "Synthetic private foreign assignment", time.Now()); err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/subscribers/audit?account="+id, nil)
		r.Header.Set("Cookie", "mikrodash_sid=synthetic-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := request()
	if w.Code != 200 || strings.Contains(w.Body.String(), "private foreign") {
		t.Fatal("cross-account chronology", w.Code, w.Body.String())
	}
	writer, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: s.auditDB.Path()}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	const base int64 = 9007199254740993
	if _, err = writer.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) VALUES(?,'synthetic-admin-id','account.credential.reset',?,'{"reason":"Synthetic safe","beforeRevision":"0","afterRevision":"1","password":"synthetic-private-material"}',0)`, base, id); err != nil {
		t.Fatal(err)
	}
	w = request()
	if w.Code != 200 || strings.Contains(w.Body.String(), "synthetic-private-material") || !strings.Contains(w.Body.String(), `"id":"9007199254740993"`) || !strings.Contains(w.Body.String(), `"evidenceState":"unavailable"`) {
		t.Fatal("unsafe or rounded audit response", w.Code, w.Body.String())
	}
	s.auditDB.Close()
	w = request()
	if w.Code != 503 || strings.Contains(w.Body.String(), "sql") || strings.Contains(w.Body.String(), s.auditDB.Path()) {
		t.Fatal("storage error leaked internals or claimed empty audit", w.Code, w.Body.String())
	}
	s.auditDB = nil
	w = request()
	if w.Code != 503 {
		t.Fatal("missing ledger not explicit", w.Code)
	}
}

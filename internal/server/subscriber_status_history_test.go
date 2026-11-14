package server

import (
	"encoding/json"
	"mikrodash/internal/subscriber"
	"net/http/httptest"
	"testing"
)

func TestSubscriberStatusHistoryAuthorityValidationAndOfflineRead(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	s.subscribers = nil // Administrative reads must work with RADIUS disabled.
	request := func(query, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/subscribers/status-history"+query, nil)
		r.Header.Set("Cookie", cookie)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	const cookie = "mikrodash_sid=synthetic-token"
	query := "?account=" + id
	if w := request(query, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if err := s.auditDB.Subscribers().SetStatus(t.Context(), "synthetic-admin", id, "suspended", "Synthetic history"); err != nil {
		t.Fatal(err)
	}
	w := request(query, cookie)
	var result subscriber.AccountStatusHistory
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.AccountID != id || len(result.Events) != 1 || result.Events[0].Reason != "Synthetic history" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, suffix := range []string{"&before=%zz", "&before=0", "&before=-1", "&before=01", "&before=+1", "&before=1.5", "&before=9223372036854775808", "&before=", "&before=1&before=2", "&account=other"} {
		if w := request(query+suffix, cookie); w.Code != 400 {
			t.Fatal(suffix, w.Code)
		}
	}
	for _, q := range []string{"", "?account=missing"} {
		if w := request(q, cookie); w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
	if w := request(query+"&before=9223372036854775807", cookie); w.Code != 200 {
		t.Fatal("exact large cursor refused", w.Code)
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "modern", Username: "unprivileged"})
	if w := request(query, cookie); w.Code != 403 {
		t.Fatal("unprivileged history", w.Code)
	}
}

func TestSubscriberStatusHistoryRejectsLegacySessionAndUnavailableLedger(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/subscribers/status-history?account="+id, nil)
		r.Header.Set("Cookie", "mikrodash_sid=synthetic-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "legacy", Username: "synthetic-admin"})
	if w := request(); w.Code != 403 {
		t.Fatal("legacy session received audit history", w.Code)
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "modern", Username: "synthetic-admin"})
	s.auditDB = nil
	if w := request(); w.Code != 503 {
		t.Fatal("unavailable ledger was not explicit", w.Code)
	}
}

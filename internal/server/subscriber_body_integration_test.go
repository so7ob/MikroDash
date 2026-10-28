package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubscriberMalformedBodyCannotCommitAccountChange(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	s.subscribers = nil
	before, err := s.auditDB.Subscribers().AccountByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"action": "status", "accountId": id, "status": "suspended", "reason": "Synthetic boundary test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{`{}`, `null`, `!`, strings.Repeat(" ", 64<<10)} {
		r := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(string(raw)+suffix))
		r.Host = "admin.example.test"
		r.Header.Set("Origin", "https://admin.example.test")
		r.Header.Set("X-MikroDash-Request", "subscriber-admin")
		r.Header.Set("Cookie", "mikrodash_sid=synthetic-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("malformed request status=%d", w.Code)
		}
		after, err := s.auditDB.Subscribers().AccountByID(t.Context(), id)
		if err != nil || after != before {
			t.Fatal("malformed request changed account/status audit", after, err)
		}
	}
}

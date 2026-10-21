package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"mikrodash/internal/subscriber"
)

func TestSubscriberStatusBatchUsesAuthorityAndAtomicConflictPath(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	s.subscribers = nil
	store := s.auditDB.Subscribers()
	other, _, err := store.CreateAccount(t.Context(), "synthetic-admin-id", "synthetic-batch-other", "Synthetic Unicode: اختبار", "synthetic-password", "Synthetic batch test", 0)
	if err != nil {
		t.Fatal(err)
	}
	untouched, _, err := store.CreateAccount(t.Context(), "synthetic-admin-id", "synthetic-not-selected", "Unselected", "synthetic-password", "Synthetic batch test", 0)
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://admin.example.test"
	const cookie = "mikrodash_sid=synthetic-token"
	request := func(body any, requestOrigin, requestCookie, marker string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(string(raw)))
		r.Host = "admin.example.test"
		r.Header.Set("Origin", requestOrigin)
		r.Header.Set("Cookie", requestCookie)
		r.Header.Set("X-MikroDash-Request", marker)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	zero := subscriber.Quantity(0)
	items := []subscriber.AccountStatusExpectation{{AccountID: id, ExpectedStatus: "active", ExpectedRevision: &zero}, {AccountID: other.ID, ExpectedStatus: "active", ExpectedRevision: &zero}}
	body := map[string]any{"action": "status-batch", "status": "suspended", "reason": "Reviewed synthetic selection", "items": items}
	for _, tc := range []struct {
		origin, cookie, marker string
		code                   int
	}{
		{origin, "", "subscriber-admin", 401},
		{"https://evil.example.test", cookie, "subscriber-admin", 403},
		{origin, cookie, "", 403},
	} {
		if w := request(body, tc.origin, tc.cookie, tc.marker); w.Code != tc.code {
			t.Fatal("batch bypassed administration/Origin guard", w.Code, tc.code)
		}
	}
	bad := map[string]any{"action": "status-batch", "status": "suspended", "reason": "Blind status change", "items": []map[string]string{{"accountId": id}}}
	if w := request(bad, origin, cookie, "subscriber-admin"); w.Code != 400 {
		t.Fatal("missing reviewed status accepted", w.Code)
	}
	for _, item := range []map[string]any{{"accountId": id, "expectedStatus": "active"}, {"accountId": id, "expectedStatus": "active", "expectedRevision": 0}} {
		bad["items"] = []map[string]any{item}
		if w := request(bad, origin, cookie, "subscriber-admin"); w.Code != 400 {
			t.Fatal("missing or numeric reviewed revision accepted", w.Code)
		}
	}
	if w := request(body, origin, cookie, "subscriber-admin"); w.Code != 200 || !strings.Contains(w.Body.String(), `"batchId"`) || strings.Contains(w.Body.String(), "password") {
		t.Fatal("atomic HTTP batch failed or exposed a credential", w.Code, w.Body.String())
	}
	if w := request(body, origin, cookie, "subscriber-admin"); w.Code != 409 || !strings.Contains(w.Body.String(), "review the entire selection again") {
		t.Fatal("stale HTTP batch was applied", w.Code, w.Body.String())
	}
	for _, selected := range []string{id, other.ID} {
		account, err := store.AccountByID(t.Context(), selected)
		if err != nil || account.Status != "suspended" {
			t.Fatal("selected account did not commit", err)
		}
	}
	current, err := store.AccountByID(t.Context(), untouched.ID)
	if err != nil || current != untouched {
		t.Fatal("unselected account changed", current, err)
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "modern", Username: "unprivileged"})
	if w := request(body, origin, cookie, "subscriber-admin"); w.Code != 403 {
		t.Fatal("unprivileged status batch accepted", w.Code)
	}
}

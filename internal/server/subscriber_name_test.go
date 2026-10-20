package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSubscriberNameEditRequiresAuthorityAndPreservesSelectedAccount(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	s.subscribers = nil
	ledger := s.auditDB.Subscribers()
	before, err := ledger.AccountByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ledger.CreateAccount(t.Context(), "synthetic-admin-id", "synthetic-other", "Other synthetic account", "synthetic-password", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	view, err := ledger.Snapshot(t.Context(), id, at)
	if err != nil {
		t.Fatal(err)
	}
	request := func(body map[string]any, origin, cookie string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/api/subscribers", strings.NewReader(string(raw)))
		r.Host = "admin.example.test"
		r.Header.Set("Origin", origin)
		r.Header.Set("X-MikroDash-Request", "subscriber-admin")
		r.Header.Set("Cookie", cookie)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	const name = "Synthetic Unicode: مشترك <script>fixture</script>"
	body := map[string]any{"action": "name", "accountId": id, "expectedName": before.Name, "name": name, "reason": "Synthetic account edit"}
	const origin = "https://admin.example.test"
	const cookie = "mikrodash_sid=synthetic-token"
	if w := request(body, origin, ""); w.Code != 401 {
		t.Fatal("unauthenticated edit", w.Code)
	}
	if w := request(body, "https://evil.example.test", cookie); w.Code != 403 {
		t.Fatal("cross-origin edit", w.Code)
	}
	if w := request(map[string]any{"action": "name", "accountId": id, "name": name, "reason": "Synthetic missing expected value"}, origin, cookie); w.Code != 400 {
		t.Fatal("blind edit accepted", w.Code)
	}
	if w := request(body, origin, cookie); w.Code != 200 {
		t.Fatal("name edit did not persist", w.Code, w.Body.String())
	}
	if w := request(body, origin, cookie); w.Code != 409 {
		t.Fatal("stale name overwrote a newer edit", w.Code, w.Body.String())
	}
	after, err := ledger.AccountByID(t.Context(), id)
	if err != nil || after.Name != name || after.Username != before.Username || after.Status != before.Status {
		t.Fatal(after, err)
	}
	unchanged, err := ledger.AccountByID(t.Context(), other.ID)
	if err != nil || unchanged != other {
		t.Fatal("another account changed", unchanged, err)
	}
	current, err := ledger.Snapshot(t.Context(), id, at)
	if err != nil || current.Version != view.Version {
		t.Fatal("metadata edit changed a subscription or balance", current, err)
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "modern", Username: "unprivileged"})
	body["expectedName"] = name
	if w := request(body, origin, cookie); w.Code != 403 {
		t.Fatal("unprivileged edit", w.Code)
	}
}

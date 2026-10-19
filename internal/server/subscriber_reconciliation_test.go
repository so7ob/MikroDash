package server

import (
	"encoding/json"
	"mikrodash/internal/db"
	"mikrodash/internal/rbac"
	"mikrodash/internal/store"
	"mikrodash/internal/subscriber"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func reconciliationFixture(t *testing.T) (*Server, *http.ServeMux, string) {
	t.Helper()
	s, a, _, _ := portalFixture(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "users.json"), []byte(`[{"id":"synthetic-admin-id","username":"synthetic-admin","role":"admin"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.store = st
	// Use a full current-schema RBAC database; the ledger remains the actual
	// isolated portal ledger so accounting and route approval share one store.
	access, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { access.Close() })
	if err = access.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "synthetic-admin-id", RoleID: "administrator", ScopeType: "global", CreatedBy: "synthetic-admin-id"}); err != nil {
		t.Fatal(err)
	}
	s.rbac = rbac.New(access, func() []rbac.Router { return []rbac.Router{} })
	s.auth = authFor("synthetic-token", &Session{AuthMode: "modern", Username: "synthetic-admin"})
	mux := http.NewServeMux()
	s.registerSubscribers(mux)
	return s, mux, a.ID
}
func TestSubscriberReconciliationAPIAuthorizationAndEmptyState(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	request := func(method, path, body, origin, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "admin.example.test"
		r.Header.Set("Origin", origin)
		r.Header.Set("X-MikroDash-Request", "subscriber-admin")
		r.Header.Set("Cookie", cookie)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	path := "/api/subscribers/reconciliation?account=" + id
	if w := request("GET", path, "", "", ""); w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := "mikrodash_sid=synthetic-token"
	if w := request("GET", path, "", "", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"gaps":[]`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/api/subscribers/reconciliation", `{"action":"legacy","id":"missing","reason":"test","evidence":"test","amount":"0"}`, "https://evil.example.test", cookie); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/api/subscribers/reconciliation", `{"action":"legacy","id":"missing","reason":"test","evidence":"test","amount":0}`, "https://admin.example.test", cookie); w.Code != 400 {
		t.Fatal("numeric JSON bypassed decimal-string contract", w.Code)
	}
	s.auth = authFor("synthetic-token", &Session{AuthMode: "modern", Username: "unprivileged"})
	if w := request("GET", path, "", "", cookie); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSubscriberReconciliationAPIFinalReadingPersistsWithoutDuplicateDebit(t *testing.T) {
	s, mux, id := reconciliationFixture(t)
	ledger := s.subscribers.radius.Store
	start := time.Now().Truncate(time.Second)
	grant, err := ledger.Reserve(t.Context(), id, "synthetic-router", "C", "02:00:00:00:00:41", "synthetic-final-review", start)
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.Account(t.Context(), subscriber.Accounting{LeaseID: grant.LeaseID, RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:41", SessionID: "synthetic-final", Status: "start", At: start}); err != nil {
		t.Fatal(err)
	}
	nas := []subscriber.NAS{{RouterID: "synthetic-router", Network: "C", CalledStationID: "synthetic-hotspot"}}
	if err = ledger.ObserveRouter(t.Context(), subscriber.RouterObservation{RouterID: "synthetic-router", At: start.Add(61 * time.Second), Complete: true, Sessions: []subscriber.ObservedSession{}}, nas); err != nil {
		t.Fatal(err)
	}
	post := func(readingID string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(map[string]any{"action": "missing-stop", "id": grant.LeaseID, "reason": "Synthetic final reading approval", "evidence": "Synthetic independent final counters", "reading": map[string]string{"leaseId": readingID, "seconds": "20", "download": "100", "upload": "0"}})
		r := httptest.NewRequest("POST", "/api/subscribers/reconciliation", strings.NewReader(string(data)))
		r.Host = "admin.example.test"
		r.Header.Set("Origin", "https://admin.example.test")
		r.Header.Set("Cookie", "mikrodash_sid=synthetic-token")
		r.Header.Set("X-MikroDash-Request", "subscriber-admin")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := post("foreign-lease"); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	for range 2 {
		if w := post(grant.LeaseID); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	view, err := ledger.Snapshot(t.Context(), id, start.Add(61*time.Second))
	if err != nil || view.LifetimeSeconds != 20 || view.LifetimeDownload != 100 || view.Decision.Limits[0].Remaining != 3580 {
		t.Fatal(view, err)
	}
}

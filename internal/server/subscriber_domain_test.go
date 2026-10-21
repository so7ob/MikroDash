package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"mikrodash/internal/hub"
	"mikrodash/internal/routeros"
	"mikrodash/internal/session"
	"mikrodash/internal/subscriber"
)

func TestPortalConnectUsesOnlyTheServerOwnedLoginDomain(t *testing.T) {
	for _, domain := range []string{"", "md-pilot"} {
		t.Run("domain="+domain, func(t *testing.T) {
			s, account, cookie, csrf := portalFixture(t)
			s.subscribers.cfg.Mode = "pilot"
			nas := subscriber.NAS{RouterID: "lab", Network: "C", CalledStationID: "hs-lab", LoginURL: "https://hotspot.example.test/login", LoginDomain: domain}
			s.subscribers.cfg.Clients = []subscriber.NAS{nas}
			h := hub.New()
			observations := 0
			reader := session.NewForTestWithExec(h, "lab", func(cmd routeros.Cmd) ([]routeros.Reply, error) {
				if cmd.Path != "/ip/hotspot/host/print" {
					t.Fatal("unexpected router command", cmd.Path)
				}
				observations++
				return []routeros.Reply{{"address": "192.0.2.1", "mac-address": "02:00:00:00:00:01", "server": "hs-lab"}}, nil
			})
			s.sessions = session.NewManagerForTest(nil, h, reader)
			handler := s.subscriberPortal(t.TempDir())
			send := func(body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "https://portal.example.test/api/connect", strings.NewReader(body))
				r.AddCookie(cookie)
				r.Header.Set("Origin", "https://portal.example.test")
				r.Header.Set("X-CSRF-Token", csrf)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			if w := send(`{"network":"C","loginDomain":"browser-choice"}`); w.Code != 400 {
				t.Fatal("browser chose authentication authority", w.Code)
			}
			w := send(`{"network":"C"}`)
			var handoff struct{ Username, Password string }
			if err := json.Unmarshal(w.Body.Bytes(), &handoff); err != nil || w.Code != 200 {
				t.Fatal("connect response unavailable", w.Code, err, w.Body.String())
			}
			expected := account.Username
			if domain != "" {
				expected = `md-pilot\` + account.Username
			}
			if handoff.Username != expected || !strings.HasPrefix(handoff.Password, "mdt_") || observations != 1 {
				t.Fatal("server domain or trusted device call site missing", handoff.Username, observations)
			}
			stored, err := s.subscribers.radius.Store.AccountByID(t.Context(), account.ID)
			if err != nil || stored.Username != account.Username {
				t.Fatal("account identity changed", err)
			}
		})
	}
}

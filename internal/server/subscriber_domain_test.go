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
			if domain != "" {
				nas.LoginRouting = &subscriber.LoginRoutingTarget{Address: "198.51.100.10", AuthPort: 11812, AccountingPort: 11813}
				nas.Capabilities = subscriber.Capabilities{Version: "synthetic-domain", Evidence: "synthetic routing evidence"}
			}
			s.subscribers.cfg.Clients = []subscriber.NAS{nas}
			h := hub.New()
			observations := 0
			reader := session.NewForTestWithExec(h, "lab", func(cmd routeros.Cmd) ([]routeros.Reply, error) {
				switch cmd.Path {
				case "/system/resource/print":
					return []routeros.Reply{{"version": "synthetic-domain"}}, nil
				case "/ip/hotspot/print":
					return []routeros.Reply{{"name": "hs-lab", "profile": "hsprof-lab", "disabled": "false"}}, nil
				case "/ip/hotspot/profile/print":
					return []routeros.Reply{{"name": "hsprof-lab", "login-by": "https", "use-radius": "true", "radius-accounting": "true", "split-user-domain": "true", "radius-default-domain": ""}}, nil
				case "/radius/print":
					return []routeros.Reply{{"domain": "md-pilot", "realm": "", "address": "198.51.100.10", "authentication-port": "11812", "accounting-port": "11813", "service": "hotspot", "disabled": "false", "protocol": "udp", "require-message-auth": "yes-for-request-resp"}}, nil
				case "/ip/hotspot/user/print":
					return []routeros.Reply{}, nil
				case "/ip/hotspot/host/print":
				default:
					t.Fatal("unexpected router command", cmd.Path)
				}
				observations++
				return []routeros.Reply{{"address": "192.0.2.1", "mac-address": "02:00:00:00:00:01", "server": "hs-lab"}}, nil
			})
			s.sessions = session.NewManagerForTest(nil, h, reader)
			s.subscribers.radius.Clients = s.subscribers.cfg.Clients
			s.subscribers.radius.ReadLoginRouting = s.subscriberReadLoginRouting
			if err := s.subscribers.radius.RefreshLoginRouting(t.Context(), "lab"); err != nil {
				t.Fatal(err)
			}
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
			if domain != "" {
				s.subscribers.radius.ReadLoginRouting = nil
				if err := s.subscribers.radius.RefreshLoginRouting(t.Context(), "lab"); err == nil {
					t.Fatal("missing metadata source was not refused")
				}
				blocked := send(`{"network":"C"}`)
				if blocked.Code != 409 || strings.Contains(blocked.Body.String(), "mdt_") {
					t.Fatal("unsafe route issued an Internet ticket", blocked.Code)
				}
			}
			stored, err := s.subscribers.radius.Store.AccountByID(t.Context(), account.ID)
			if err != nil || stored.Username != account.Username {
				t.Fatal("account identity changed", err)
			}
		})
	}
}

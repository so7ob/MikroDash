//go:build subscriberbrowser

package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/hub"
	"mikrodash/internal/routeros"
	"mikrodash/internal/session"
	"mikrodash/internal/subscriber"
)

// Real browser HTTPS POST/return and protected SQLite APIs; NAS decisions and
// observations are synthetic. This never opens a RouterOS connection or UDP socket.
func TestSubscriberHandoffBrowser(t *testing.T) {
	if os.Getenv("SUBSCRIBER_BROWSER_MODULE") == "" || os.Getenv("SUBSCRIBER_EVIDENCE_DIR") == "" {
		t.Fatal("configure browser module and evidence directory")
	}
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprintf("denied=%t", denied), func(t *testing.T) {
			s, account, _, _ := portalFixture(t)
			s.subscribers.cfg.Mode = "pilot"
			s.subscribers.radius.Mode = "pilot"
			ledger := s.auditDB.Subscribers()
			root, err := filepath.Abs("../..")
			if err != nil {
				t.Fatal(err)
			}
			s.staticDir = filepath.Join(root, "web/public")
			device := "02:00:00:00:00:88"
			nas := subscriber.NAS{RouterID: "lab", Network: "C", CalledStationID: "hs-lab", Capabilities: subscriber.Capabilities{Version: "synthetic-88", Evidence: "synthetic browser observation", CounterMode: "input-upload"}}
			hotspot := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/login" {
					http.NotFound(w, r)
					return
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
					http.Error(w, "invalid form", 400)
					return
				}
				token := r.Form.Get("password")
				flow := subscriber.Digest(token)
				if !strings.HasPrefix(token, "mdt_") || r.Form.Get("username") != account.Username || r.Form.Get("dst") != s.subscribers.cfg.PortalOrigin+"/?connection="+flow || r.Form.Get("popup") != "false" {
					t.Error("browser sent an invalid owned handoff")
					http.Error(w, "invalid handoff", 400)
					return
				}
				at := time.Now().Truncate(time.Second)
				binding := subscriber.TicketBinding{RouterID: "lab", Network: "C", Device: device, Username: account.Username}
				request := "synthetic-browser-" + flow
				if _, err := ledger.RedeemTicket(r.Context(), token, binding, request, at); err != nil {
					t.Error(err)
					http.Error(w, "invalid ticket", 400)
					return
				}
				outcome := subscriber.TicketOutcome{Reasons: []string{"exhausted:باقة تجريبية"}}
				if !denied {
					grant, err := ledger.Reserve(r.Context(), account.ID, "lab", "C", device, request, at)
					if err != nil {
						t.Error(err)
						http.Error(w, "grant failed", 500)
						return
					}
					outcome = subscriber.TicketOutcome{LeaseID: grant.LeaseID}
					if err := ledger.Account(r.Context(), subscriber.Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: device, SessionID: "synthetic-browser", Status: "start", Source: "radius", At: at}); err != nil {
						t.Error(err)
						http.Error(w, "account failed", 500)
						return
					}
				}
				if err := ledger.RecordTicketOutcome(r.Context(), token, binding, request, outcome, at); err != nil {
					t.Error(err)
					http.Error(w, "outcome failed", 500)
					return
				}
				if !denied {
					observation := subscriber.RouterObservation{RouterID: "lab", Version: "synthetic-88", At: at, Complete: true, Sessions: []subscriber.ObservedSession{{ID: "*88", Server: "hs-lab", Username: account.Username, Device: device, Address: "127.0.0.1", CountersKnown: true, TimeoutKnown: true, TimeLeft: 55}}}
					if err := ledger.ObserveRouter(r.Context(), observation, []subscriber.NAS{nas}); err != nil {
						t.Error(err)
						http.Error(w, "observation failed", 500)
						return
					}
				}
				http.Redirect(w, r, r.Form.Get("dst"), http.StatusSeeOther)
			}))
			t.Cleanup(hotspot.Close)
			nas.LoginURL = hotspot.URL + "/login"
			s.subscribers.cfg.Clients = []subscriber.NAS{nas}
			s.subscribers.radius.Clients = []subscriber.NAS{nas}
			h := hub.New()
			reader := session.NewForTestWithExec(h, "lab", func(cmd routeros.Cmd) ([]routeros.Reply, error) {
				if cmd.Path != "/ip/hotspot/host/print" {
					return nil, fmt.Errorf("unexpected synthetic router read: %s", cmd.Path)
				}
				return []routeros.Reply{{"address": "127.0.0.1", "mac-address": device, "server": "hs-lab"}}, nil
			})
			s.sessions = session.NewManagerForTest(nil, h, reader)
			edge := httptest.NewTLSServer(s.subscriberPortal(filepath.Join(root, "web/dist")))
			t.Cleanup(edge.Close)
			s.subscribers.cfg.PortalOrigin = edge.URL
			command := exec.Command("node", filepath.Join(root, "tools/subscriber-portal-handoff-browser.mjs"))
			command.Env = append(os.Environ(), "SUBSCRIBER_TEST_URL="+edge.URL, "SUBSCRIBER_TEST_HOTSPOT="+hotspot.URL, fmt.Sprintf("SUBSCRIBER_TEST_DENIED=%t", denied))
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("browser handoff: %v\n%s", err, output)
			}
			t.Log(string(output))
		})
	}
}

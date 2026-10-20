//go:build subscriberbrowser

package server

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"mikrodash/internal/subscriber"
)

// Run explicitly with -tags=subscriberbrowser and the external browser harness
// configured. A missing harness fails; it never silently skips acceptance.
func TestSubscriberBrowserAcceptance(t *testing.T) {
	if os.Getenv("SUBSCRIBER_BROWSER_MODULE") == "" || os.Getenv("SUBSCRIBER_EVIDENCE_DIR") == "" {
		t.Fatal("configure browser module and evidence directory")
	}
	for _, tc := range []struct {
		transport string
		pending   bool
	}{{"tls", false}, {"trusted-proxy-http", false}, {"tls", true}, {"trusted-proxy-http", true}} {
		name := tc.transport
		if tc.pending {
			name += "/pending-receipt"
		}
		t.Run(name, func(t *testing.T) {
			s, account, _, _ := portalFixture(t)
			if tc.pending {
				ledger := s.auditDB.Subscribers()
				start := time.Now()
				grant, err := ledger.Reserve(t.Context(), account.ID, "synthetic-router", "C", "02:00:00:00:00:42", "synthetic-browser-request", start)
				if err != nil {
					t.Fatal(err)
				}
				if err = ledger.Account(t.Context(), subscriber.Accounting{LeaseID: grant.LeaseID, RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:42", SessionID: "synthetic-browser-session", Status: "stop", Source: "radius", At: start.Add(20 * time.Second), Seconds: 20, Download: 100}); err != nil {
					t.Fatal(err)
				}
				receipt := subscriber.AccountingReceipt{ID: "synthetic-portal-receipt", LeaseID: grant.LeaseID, RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:42", SessionID: "synthetic-browser-session", Status: "stop", Seconds: "20", TimeKnown: true, Download: "101", DownloadKnown: true, At: start.Add(21 * time.Second), ReceivedAt: start.Add(21 * time.Second), Fields: map[string]string{"Acct-Session-Time": "20", "Acct-Output-Octets": "101"}}
				if ack, err := ledger.ReceiveAccounting(t.Context(), receipt); err != nil || !ack {
					t.Fatal(ack, err)
				}
			}
			root, err := filepath.Abs("../..")
			if err != nil {
				t.Fatal(err)
			}
			s.staticDir = filepath.Join(root, "web/public")
			var origin string
			evidence := os.Getenv("SUBSCRIBER_EVIDENCE_DIR")
			if tc.transport == "trusted-proxy-http" {
				edge, _ := subscriberProxyFixture(t, s, filepath.Join(root, "web/dist"))
				origin = edge.URL
				evidence = filepath.Join(evidence, "proxy")
			} else {
				server := httptest.NewTLSServer(s.subscriberPortal(filepath.Join(root, "web/dist")))
				t.Cleanup(server.Close)
				s.subscribers.cfg.PortalOrigin = server.URL
				origin = server.URL
			}
			command := exec.Command("node", filepath.Join(root, "tools/subscriber-browser.mjs"))
			if tc.pending {
				evidence = filepath.Join(evidence, "pending-receipt")
			}
			command.Env = append(os.Environ(), "SUBSCRIBER_TEST_URL="+origin, "SUBSCRIBER_EVIDENCE_DIR="+evidence, fmt.Sprintf("SUBSCRIBER_PENDING_RECEIPT=%t", tc.pending))
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("browser acceptance: %v\n%s", err, output)
			}
			t.Log(string(output))
		})
	}
}

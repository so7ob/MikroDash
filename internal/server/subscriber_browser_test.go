//go:build subscriberbrowser

package server

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Run explicitly with -tags=subscriberbrowser and the external browser harness
// configured. A missing harness fails; it never silently skips acceptance.
func TestSubscriberBrowserAcceptance(t *testing.T) {
	if os.Getenv("SUBSCRIBER_BROWSER_MODULE") == "" || os.Getenv("SUBSCRIBER_EVIDENCE_DIR") == "" {
		t.Fatal("configure browser module and evidence directory")
	}
	s, _, _, _ := portalFixture(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.staticDir = filepath.Join(root, "web/public")
	server := httptest.NewTLSServer(s.subscriberPortal(filepath.Join(root, "web/dist")))
	defer server.Close()
	s.subscribers.cfg.PortalOrigin = server.URL
	command := exec.Command("node", filepath.Join(root, "tools/subscriber-browser.mjs"))
	command.Env = append(os.Environ(), "SUBSCRIBER_TEST_URL="+server.URL)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("browser acceptance: %v\n%s", err, output)
	}
	t.Log(string(output))
}

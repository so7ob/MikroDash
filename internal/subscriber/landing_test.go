package subscriber

import (
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func landingFixture() Config {
	return Config{Mode: "shadow", PortalOrigin: "https://portal.example.test:9444", PortalListen: "127.0.0.1:9444", TLSCert: "synthetic.pem", TLSKey: "synthetic-key.pem", Clients: []NAS{{RouterID: "lab", Network: "C", Address: "127.0.0.1", CalledStationID: "hs-lab", LoginURL: "https://hotspot.example.test/login", DisconnectPort: 3799, Secret: testSecret}}}
}

func TestHotspotLandingKeepsSecretsOutAndPreservesEncodedNetworkData(t *testing.T) {
	cfg := landingFixture()
	cfg.Clients[0].Network = "شبكة سحب $(username)"
	script := []byte("(()=>{ /* synthetic handoff asset */ })();")
	bundle, err := RenderHotspotLanding(cfg, cfg.Clients[0].Network, script)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Files) != 12 {
		t.Fatal("missing servlet/asset coverage", len(bundle.Files))
	}
	script[0] = 'x'
	if bundle.Files["subscriber-handoff.js"][0] == 'x' {
		t.Fatal("rendered asset aliases mutable input")
	}
	for name, body := range bundle.Files {
		if len(body) == 0 || len(body) > 4096 || strings.Contains(string(body), testSecret) || strings.Contains(string(body), "synthetic-key.pem") || strings.Contains(string(body), "$(username)") {
			t.Fatal("unsafe or out-of-budget public asset", name)
		}
		if strings.HasSuffix(name, ".html") {
			source := string(body)
			for _, required := range []string{`lang="en" dir="ltr"`, `data-return="$(link-redirect-esc)"`, `data-original="$(link-orig-esc)"`, `src="subscriber-handoff.js"`, `Continue to subscriber portal`} {
				if !strings.Contains(source, required) {
					t.Fatal("handoff contract missing", name, required)
				}
			}
			if strings.Contains(source, "$(error)") || strings.Contains(source, "<script>") || strings.Contains(source, "window.open") {
				t.Fatal("raw router input or inline popup script", name)
			}
		}
	}
	re := regexp.MustCompile(`id="subscriberPortalLink"[^>]+href="([^"]+)"`)
	match := re.FindSubmatch(bundle.Files["login.html"])
	if len(match) != 2 {
		t.Fatal("no immediate portal link")
	}
	link, err := url.Parse(html.UnescapeString(string(match[1])))
	if err != nil || link.Query().Get("connect") != cfg.Clients[0].Network {
		t.Fatal("network data was translated, changed or left as a RouterOS macro", link, err)
	}
	manifest, err := bundle.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Network string         `json:"network"`
		Files   []LandingAsset `json:"files"`
	}
	if err = json.Unmarshal(manifest, &meta); err != nil || meta.Network != cfg.Clients[0].Network || len(meta.Files) != len(bundle.Files) {
		t.Fatal("invalid public manifest", err)
	}
	for _, asset := range meta.Files {
		if asset.SHA256 != Digest(string(bundle.Files[asset.Name])) || asset.Bytes != len(bundle.Files[asset.Name]) {
			t.Fatal("manifest does not cover the actual bytes", asset.Name)
		}
	}
	api := string(bundle.Files["api.json"])
	if !strings.Contains(api, `"captive": $(if logged-in == 'yes')false$(else)true$(endif)`) || !strings.Contains(api, `"user-portal-url":`) {
		t.Fatal("captive API contract missing")
	}
}

func TestHotspotLandingRejectsAmbiguousUnsafeAndUnbuiltInputs(t *testing.T) {
	for _, tc := range []string{"unknown-network", "duplicate-network", "http-origin", "origin-credentials", "origin-query", "origin-macro", "empty-script", "large-script"} {
		t.Run(tc, func(t *testing.T) {
			cfg := landingFixture()
			network := "C"
			script := []byte("synthetic")
			switch tc {
			case "unknown-network":
				network = "D"
			case "duplicate-network":
				cfg.Clients = append(cfg.Clients, cfg.Clients[0])
			case "http-origin":
				cfg.PortalOrigin = "http://portal.example.test"
			case "origin-credentials":
				cfg.PortalOrigin = "https://user:pass@portal.example.test"
			case "origin-query":
				cfg.PortalOrigin = "https://portal.example.test?return=foreign"
			case "origin-macro":
				cfg.PortalOrigin = "https://$(username).example.test"
			case "empty-script":
				script = nil
			case "large-script":
				script = make([]byte, 4097)
			}
			if _, err := RenderHotspotLanding(cfg, network, script); err == nil {
				t.Fatal("invalid handoff accepted", tc)
			}
		})
	}
}

package main

import (
	"encoding/json"
	"mikrodash/internal/subscriber"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderCommandRequiresBuiltAssetsAndNeverPublishesSecrets(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.json")
	web := filepath.Join(root, "web")
	output := filepath.Join(root, "review")
	cfg := subscriber.Config{Mode: "shadow", PortalOrigin: "https://portal.example.test:9444", PortalListen: "127.0.0.1:9444", TLSCert: "synthetic.pem", TLSKey: "synthetic-key.pem", Clients: []subscriber.NAS{{RouterID: "lab", Network: "C", Address: "127.0.0.1", CalledStationID: "hs-lab", LoginURL: "https://hotspot.example.test/login", DisconnectPort: 3799, SecretSealed: "synthetic-sealed-secret"}}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = render(config, "C", web, output); err == nil {
		t.Fatal("unbuilt handoff accepted")
	}
	if _, err = os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("unbuilt output was published", err)
	}
	if err = os.Mkdir(web, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(web, "subscriber-handoff.js"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = render(config, "C", web, output); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(output, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err = json.Unmarshal(manifest, &metadata); err != nil || metadata["network"] != "C" {
		t.Fatal("invalid rendered command output", err)
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 13 {
		t.Fatal("empty or incomplete command output", err)
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(output, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), cfg.Clients[0].SecretSealed) || strings.Contains(string(body), cfg.TLSKey) || strings.Contains(string(body), "secretSealed") {
			t.Fatal("private configuration material copied into public output")
		}
	}
}

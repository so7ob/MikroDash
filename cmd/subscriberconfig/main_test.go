package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mikrodash/internal/store"
	"mikrodash/internal/subscriber"
)

func TestSealUsesExistingKeyAndNeverOverwrites(t *testing.T) {
	t.Setenv("DATA_SECRET", "")
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "routers.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := subscriber.Config{Mode: "shadow", PortalListen: "127.0.0.1:9444", PortalOrigin: "https://portal.example.test", TLSCert: "test.pem", TLSKey: "test.key", Clients: []subscriber.NAS{{RouterID: "synthetic-router", Network: "C", Address: "192.0.2.1", CalledStationID: "synthetic-hotspot", DisconnectPort: 3799, LoginURL: "https://hotspot.example.test/login"}}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	input, output := filepath.Join(dir, "template.json"), filepath.Join(dir, "sealed.json")
	if err = os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	secret := []byte(subscriber.ID())
	if err = seal(dir, input, output, "synthetic-router", "C", bytes.NewReader(secret)); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(output)
	if err != nil || bytes.Contains(raw, secret) {
		t.Fatal("sealed output missing or contains plaintext")
	}
	var got subscriber.Config
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	plain, err := st.Decrypt(got.Clients[0].SecretSealed)
	if err != nil || plain != string(secret) {
		t.Fatal("existing key cannot recover secret")
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("output must be private")
	}
	if seal(dir, input, output, "synthetic-router", "C", bytes.NewReader(secret)) == nil {
		t.Fatal("overwrote existing output")
	}
	after, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("existing output changed")
	}
	if seal(dir, input, filepath.Join(dir, "wrong.json"), "another-router", "C", bytes.NewReader(secret)) == nil {
		t.Fatal("unknown NAS accepted")
	}
	if seal(t.TempDir(), input, filepath.Join(dir, "new.json"), "synthetic-router", "C", bytes.NewReader(secret)) == nil {
		t.Fatal("uninitialized store accepted")
	}
}

package server

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mikrodash/internal/hub"
	"mikrodash/internal/routeros"
	"mikrodash/internal/session"
	"mikrodash/internal/store"
	"mikrodash/internal/subscriber"
)

func TestSubscriberStartupServesSelectedTransportAndStopsWithoutWatchers(t *testing.T) {
	for _, transport := range []string{"tls", subscriber.PortalTrustedProxyHTTP} {
		t.Run(transport, func(t *testing.T) {
			s, _, _, _ := portalFixture(t)
			s.subscribers = nil
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			s.store = st
			sealed, err := st.Encrypt("synthetic-startup-radius-secret")
			if err != nil {
				t.Fatal(err)
			}
			cfg := subscriber.Config{Mode: "shadow", PortalListen: "127.0.0.1:0", PortalOrigin: "https://portal.example.test", PortalTransport: transport, AuthListen: "127.0.0.1:0", AccountingListen: "127.0.0.1:0", Clients: []subscriber.NAS{{RouterID: "synthetic-router", Network: "C", Address: "127.0.0.1", SecretSealed: sealed, CalledStationID: "hs-lab", DisconnectPort: 3799, LoginURL: "https://hotspot.example.test/login"}}}
			client := &http.Client{Timeout: 5 * time.Second}
			if cfg.UsesPortalProxy() {
				cfg.PortalProxyPeers = []string{"127.0.0.2"}
				dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}}
				tr := &http.Transport{DialContext: dialer.DialContext}
				client.Transport = tr
				t.Cleanup(tr.CloseIdleConnections)
			} else {
				identity := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				t.Cleanup(identity.Close)
				client = identity.Client()
				client.Timeout = 5 * time.Second
				dir := t.TempDir()
				cfg.TLSCert = filepath.Join(dir, "synthetic.pem")
				cfg.TLSKey = filepath.Join(dir, "synthetic.key")
				private, err := x509.MarshalPKCS8PrivateKey(identity.TLS.Certificates[0].PrivateKey)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(cfg.TLSCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: identity.Certificate().Raw}), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(cfg.TLSKey, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
					t.Fatal(err)
				}
			}
			observed := make(chan struct{}, 8)
			h := hub.New()
			scripted := session.NewForTestWithExec(h, "synthetic-router", func(cmd routeros.Cmd) ([]routeros.Reply, error) {
				if cmd.Path == "/system/resource/print" {
					select {
					case observed <- struct{}{}:
					default:
					}
					return []routeros.Reply{{"version": "synthetic-lab-version"}}, nil
				}
				return []routeros.Reply{}, nil
			})
			manager := session.NewManagerForTest(st, h, scripted)
			s.sessions = manager
			stopped := false
			stop := func() {
				if !stopped && s.subscribers != nil {
					_, _ = manager.Acquire("synthetic-router")
					s.stopSubscribers()
					stopped = true
				}
			}
			t.Cleanup(stop)
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(t.TempDir(), "subscriber.json")
			if err = os.WriteFile(config, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err = s.StartSubscribers(config, t.TempDir()); err != nil {
				t.Fatal("actual runtime startup failed", err)
			}
			rt := s.subscribers
			if rt == nil || len(manager.Held("synthetic-router")) != 1 {
				t.Fatal("runtime did not retain background NAS session")
			}
			scheme := "https"
			if cfg.UsesPortalProxy() {
				scheme = "http"
			}
			request, err := http.NewRequest("GET", scheme+"://"+rt.http.Addr+"/api/account", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Host = "portal.example.test"
			request.Header.Set("X-Forwarded-Proto", "https")
			request.Header.Set("X-Forwarded-For", "198.51.100.20")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal("startup did not serve selected transport", err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode != 401 {
				t.Fatal(response.StatusCode)
			}
			if (response.TLS == nil) != cfg.UsesPortalProxy() {
				t.Fatal("unexpected backend transport")
			}
			if response.TLS != nil && response.TLS.Version < 0x0303 {
				t.Fatal("native TLS minimum weakened")
			}
			// Remove the scripted viewer reference: no browser or WebSocket keeps the worker alive.
			manager.Release("synthetic-router")
			client.CloseIdleConnections()
			select {
			case <-observed:
			case <-time.After(6 * time.Second):
				t.Fatal("background observer stopped without watchers")
			}
			stop()
			select {
			case <-rt.done:
			default:
				t.Fatal("worker did not finish during shutdown")
			}
			if len(manager.Held("synthetic-router")) != 0 {
				t.Fatal("subscriber hold leaked after shutdown")
			}
			conn, err := net.DialTimeout("tcp", rt.http.Addr, time.Second)
			if err == nil {
				conn.Close()
				t.Fatal("portal listener survived shutdown")
			}
			if rt.ctx.Err() != context.Canceled {
				t.Fatal("runtime context still active")
			}
		})
	}
}

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mikrodash/internal/subscriber"
)

func proxyTestConfig() subscriber.Config {
	return subscriber.Config{Mode: "shadow", PortalOrigin: "https://portal.example.test", PortalListen: "127.0.0.1:0", PortalTransport: subscriber.PortalTrustedProxyHTTP, PortalProxyPeers: []string{"127.0.0.2", "127.0.0.3"}}
}

func TestSubscriberProxyBoundaryAuthenticatesPeerAndClient(t *testing.T) {
	cfg := proxyTestConfig()
	handler := subscriberProxyBoundary(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(subscriberClientIPOf(r))) }))
	cases := []struct {
		name, peer, host, proto string
		ff                      []string
		code                    int
		client                  string
	}{
		{"real client", "127.0.0.2:8", "portal.example.test", "https", []string{"198.51.100.20"}, 200, "198.51.100.20"},
		{"injected left claim", "127.0.0.2:8", "portal.example.test", "https", []string{"198.51.100.66, 198.51.100.20"}, 200, "198.51.100.20"},
		{"exact trusted chain", "127.0.0.2:8", "portal.example.test", "https", []string{"198.51.100.20, 127.0.0.3"}, 200, "198.51.100.20"},
		{"untrusted peer", "127.0.0.4:8", "portal.example.test", "https", []string{"198.51.100.20"}, 403, ""},
		{"malformed peer", "unavailable", "portal.example.test", "https", []string{"198.51.100.20"}, 403, ""},
		{"wrong host", "127.0.0.2:8", "admin.example.test", "https", []string{"198.51.100.20"}, 403, ""},
		{"wrong scheme", "127.0.0.2:8", "portal.example.test", "http", []string{"198.51.100.20"}, 403, ""},
		{"missing scheme", "127.0.0.2:8", "portal.example.test", "", []string{"198.51.100.20"}, 403, ""},
		{"ambiguous scheme", "127.0.0.2:8", "portal.example.test", "https,http", []string{"198.51.100.20"}, 403, ""},
		{"missing forwarded client", "127.0.0.2:8", "portal.example.test", "https", nil, 403, ""},
		{"duplicate forwarded fields", "127.0.0.2:8", "portal.example.test", "https", []string{"198.51.100.20", "198.51.100.66"}, 403, ""},
		{"malformed hop", "127.0.0.2:8", "portal.example.test", "https", []string{"malformed, 198.51.100.20"}, 403, ""},
		{"unresolved proxy only", "127.0.0.2:8", "portal.example.test", "https", []string{"127.0.0.3"}, 403, ""},
		{"unspecified", "127.0.0.2:8", "portal.example.test", "https", []string{"0.0.0.0"}, 403, ""},
		{"mapped unspecified", "127.0.0.2:8", "portal.example.test", "https", []string{"::ffff:0.0.0.0"}, 403, ""},
		{"multicast", "127.0.0.2:8", "portal.example.test", "https", []string{"224.0.0.1"}, 403, ""},
		{"mapped multicast", "127.0.0.2:8", "portal.example.test", "https", []string{"::ffff:224.0.0.1"}, 403, ""},
		{"too many hops", "127.0.0.2:8", "portal.example.test", "https", []string{strings.Repeat("198.51.100.20,", 33) + "198.51.100.20"}, 403, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			r.Host = tc.host
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			r.Header["X-Forwarded-For"] = tc.ff
			r.Header.Set("X-Real-IP", "198.51.100.66")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.code || (tc.code == 200 && w.Body.String() != tc.client) {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.2:8"
	r.Host = "portal.example.test"
	r.Header["X-Forwarded-Proto"] = []string{"https", "https"}
	r.Header.Set("X-Forwarded-For", "198.51.100.20")
	duplicate := httptest.NewRecorder()
	handler.ServeHTTP(duplicate, r)
	if duplicate.Code != 403 {
		t.Fatal("duplicate forwarded scheme was accepted", duplicate.Code)
	}
	bad := cfg
	bad.PortalProxyPeers = []string{"0.0.0.0/0"}
	w := httptest.NewRecorder()
	subscriberProxyBoundary(bad, handler).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 503 {
		t.Fatal("invalid configuration did not fail closed", w.Code)
	}
}

func FuzzSubscriberProxyNeverTrustsAnInjectedLeftClaim(f *testing.F) {
	for _, v := range []string{"198.51.100.66", "127.0.0.2", "malformed", "198.51.100.66,127.0.0.3", "::ffff:0.0.0.0"} {
		f.Add(v)
	}
	handler := subscriberProxyBoundary(proxyTestConfig(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(subscriberClientIPOf(r))) }))
	f.Fuzz(func(t *testing.T, claim string) {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "127.0.0.2:8"
		r.Host = "portal.example.test"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-For", claim+",198.51.100.20")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 && (w.Code != 200 || w.Body.String() != "198.51.100.20") {
			t.Fatal("injected claim changed client identity", w.Code, w.Body.String())
		}
	})
}

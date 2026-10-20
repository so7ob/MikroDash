package subscriber

import (
	"encoding/json"
	"testing"
)

func TestPortalProxyConfigurationIsExplicitAndPrivate(t *testing.T) {
	base := Config{Mode: "shadow", PortalOrigin: "https://portal.example.test:9443", PortalListen: "127.0.0.1:38101", PortalTransport: "trusted-proxy-http", PortalProxyPeers: []string{"127.0.0.2"}}
	if err := base.Validate(); err != nil {
		t.Fatalf("explicit isolated HTTP backend behind HTTPS proxy was refused: %v", err)
	}
	cases := []struct {
		name   string
		change func(*Config)
	}{
		{"unknown transport", func(c *Config) { c.PortalTransport = "http" }},
		{"implicit plaintext", func(c *Config) { c.PortalTransport = "" }},
		{"public bind", func(c *Config) { c.PortalListen = "198.51.100.1:38101" }},
		{"wildcard bind", func(c *Config) { c.PortalListen = "0.0.0.0:38101" }},
		{"IPv6 wildcard", func(c *Config) { c.PortalListen = "[::]:38101" }},
		{"hostname bind", func(c *Config) { c.PortalListen = "localhost:38101" }},
		{"missing peers", func(c *Config) { c.PortalProxyPeers = nil }},
		{"CIDR peers", func(c *Config) { c.PortalProxyPeers = []string{"127.0.0.0/8"} }},
		{"wildcard peers", func(c *Config) { c.PortalProxyPeers = []string{"0.0.0.0"} }},
		{"public peers", func(c *Config) { c.PortalProxyPeers = []string{"198.51.100.2"} }},
		{"duplicate mapped peers", func(c *Config) { c.PortalProxyPeers = []string{"127.0.0.2", "::ffff:127.0.0.2"} }},
		{"plaintext origin", func(c *Config) { c.PortalOrigin = "http://portal.example.test" }},
		{"mixed TLS identity", func(c *Config) { c.TLSCert = "unused.pem"; c.TLSKey = "unused.key" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.change(&c)
			if c.Validate() == nil {
				t.Fatal("unsafe or ambiguous transport accepted")
			}
		})
	}
	tls := base
	tls.PortalTransport = ""
	tls.PortalProxyPeers = nil
	tls.TLSCert = "portal.pem"
	tls.TLSKey = "portal.key"
	if err := tls.Validate(); err != nil {
		t.Fatal("existing TLS configuration changed", err)
	}
	for _, transport := range []string{"", "tls"} {
		tls.PortalTransport = transport
		tls.TLSCert = ""
		if tls.Validate() == nil {
			t.Fatal("missing native identity accepted")
		}
		tls.TLSCert = "portal.pem"
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeConfig(raw); err != nil {
		t.Fatal("configuration decoder does not consume transport", err)
	}
}

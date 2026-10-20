package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

// The lab uses separate loopback addresses for the HTTPS client and private
// proxy peer. Only the NAS responses are scripted; HTTP/TLS and SQLite are real.
func subscriberProxyFixture(t *testing.T, s *Server, webDir string) (*httptest.Server, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	edge := httptest.NewTLSServer(mux)
	t.Cleanup(edge.Close)
	s.subscribers.cfg.PortalOrigin = edge.URL
	s.subscribers.cfg.PortalTransport = "trusted-proxy-http"
	s.subscribers.cfg.PortalListen = "127.0.0.1:0"
	s.subscribers.cfg.PortalProxyPeers = []string{"127.0.0.2"}
	backend := httptest.NewServer(s.subscriberPortal(webDir))
	t.Cleanup(backend.Close)
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, address)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	proxy := &httputil.ReverseProxy{Transport: transport, Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(target)
		p.Out.Host = p.In.Host
		// Model a trusted edge that appends its actual client after a forged claim.
		if v := p.In.Header.Values("X-Forwarded-For"); len(v) > 0 {
			p.Out.Header["X-Forwarded-For"] = v
		}
		p.SetXForwarded()
	}}
	mux.Handle("/", proxy)
	return edge, backend
}

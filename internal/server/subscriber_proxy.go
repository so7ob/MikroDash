package server

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"mikrodash/internal/subscriber"
	"mikrodash/internal/trustedproxy"
)

type subscriberClientIPKey struct{}

func subscriberClientIPOf(r *http.Request) string {
	if ip, ok := r.Context().Value(subscriberClientIPKey{}).(string); ok {
		return ip
	}
	return clientIPOf(r)
}

// subscriberProxyBoundary is local to the isolated portal. It neither changes
// the admin listener's proxy trust nor accepts browser-supplied device claims.
func subscriberProxyBoundary(cfg subscriber.Config, next http.Handler) http.Handler {
	if cfg.PortalTransport == "" || cfg.PortalTransport == "tls" {
		return next
	}
	peers, err := cfg.PortalProxyAddresses()
	origin, uerr := url.Parse(cfg.PortalOrigin)
	if err != nil || uerr != nil || !cfg.UsesPortalProxy() || cfg.Validate() != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSONErr(w, 503, "Subscriber proxy configuration unavailable")
		})
	}
	prefixes := make([]netip.Prefix, 0, len(peers))
	for _, a := range peers {
		prefixes = append(prefixes, netip.PrefixFrom(a, a.BitLen()))
	}
	trusted := func(a netip.Addr) bool {
		for _, p := range peers {
			if p == a.Unmap() {
				return true
			}
		}
		return false
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		peer, perr := netip.ParseAddr(host)
		if err != nil || perr != nil || peer.Zone() != "" || !trusted(peer) {
			writeJSONErr(w, 403, "Portal proxy peer rejected")
			return
		}
		proto := r.Header.Values("X-Forwarded-Proto")
		if len(proto) != 1 || strings.TrimSpace(proto[0]) != "https" || !strings.EqualFold(r.Host, origin.Host) {
			writeJSONErr(w, 403, "Portal proxy origin rejected")
			return
		}
		ff := r.Header.Values("X-Forwarded-For")
		if len(ff) != 1 || len(ff[0]) > 4096 || !validSubscriberForwarding(ff[0]) {
			writeJSONErr(w, 403, "Portal client address unavailable")
			return
		}
		ip := trustedproxy.Client(r.RemoteAddr, ff, prefixes)
		client, cerr := netip.ParseAddr(ip)
		if cerr != nil || trusted(client) {
			writeJSONErr(w, 403, "Portal client address unavailable")
			return
		}
		ctx := context.WithValue(r.Context(), subscriberClientIPKey{}, client.Unmap().String())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Validate every hop, then let the shared attribution parser walk from the
// right. A client-injected leftmost address never overrides the actual client.
func validSubscriberForwarding(raw string) bool {
	hops := strings.Split(raw, ",")
	if len(hops) > 32 {
		return false
	}
	for _, hop := range hops {
		v := strings.TrimSpace(hop)
		if h, _, err := net.SplitHostPort(v); err == nil {
			v = h
		}
		a, err := netip.ParseAddr(v)
		if err != nil || a.Zone() != "" {
			return false
		}
		a = a.Unmap()
		if a.IsUnspecified() || a.IsMulticast() {
			return false
		}
	}
	return true
}

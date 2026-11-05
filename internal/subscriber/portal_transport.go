package subscriber

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
)

const PortalTrustedProxyHTTP = "trusted-proxy-http"

// UsesPortalProxy never infers plaintext permission from missing TLS files.
func (c Config) UsesPortalProxy() bool { return c.PortalTransport == PortalTrustedProxyHTTP }

// PortalProxyAddresses accepts exact private peers, not a range of possible clients.
func (c Config) PortalProxyAddresses() ([]netip.Addr, error) {
	if len(c.PortalProxyPeers) == 0 || len(c.PortalProxyPeers) > 8 {
		return nil, errors.New("configure one to eight exact portal proxy peers")
	}
	out := make([]netip.Addr, 0, len(c.PortalProxyPeers))
	seen := map[netip.Addr]bool{}
	for _, raw := range c.PortalProxyPeers {
		a, err := netip.ParseAddr(raw)
		if err != nil || a.Zone() != "" {
			return nil, errors.New("portal proxy peers must be literal private or loopback addresses")
		}
		a = a.Unmap()
		if (!a.IsPrivate() && !a.IsLoopback()) || seen[a] {
			return nil, errors.New("portal proxy peers must be unique private or loopback addresses")
		}
		seen[a] = true
		out = append(out, a)
	}
	return out, nil
}

func (c Config) validatePortalTransport() error {
	if c.PortalListen == "" {
		return errors.New("separate portal listener required")
	}
	switch c.PortalTransport {
	case "", "tls":
		if c.TLSCert == "" || c.TLSKey == "" {
			return errors.New("separate portal TLS listener required")
		}
		if len(c.PortalProxyPeers) != 0 {
			return errors.New("portal proxy peers require trusted-proxy-http transport")
		}
	case PortalTrustedProxyHTTP:
		if c.TLSCert != "" || c.TLSKey != "" {
			return errors.New("proxy HTTP transport must not configure an unused TLS identity")
		}
		host, port, err := net.SplitHostPort(c.PortalListen)
		if err != nil {
			return errors.New("portal proxy backend requires a literal private bind address and port")
		}
		a, err := netip.ParseAddr(host)
		if err != nil || a.Zone() != "" {
			return errors.New("portal proxy backend requires a literal private bind address")
		}
		a = a.Unmap()
		if !a.IsPrivate() && !a.IsLoopback() {
			return errors.New("portal proxy backend must bind a private or loopback address")
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return errors.New("invalid portal proxy backend port")
		}
		if _, err = c.PortalProxyAddresses(); err != nil {
			return err
		}
	default:
		return errors.New("unsupported portal transport")
	}
	return nil
}

package subscriberlab

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"mikrodash/internal/routeros"
)

type FixtureCredentials struct {
	Secret   string `json:"secret"`
	Password string `json:"password"`
}

// Bootstrap creates only the synthetic appliance's C-like HotSpot. Initial PAP
// is confined to the isolated namespace for protocol probes; it is not the HTTPS
// portal acceptance path and must never be copied onto an operator's network.
func (c *Client) Bootstrap(privateDir string) error {
	var fixture FixtureCredentials
	if err := privateJSON(filepath.Join(privateDir, "fixture.json"), &fixture); err != nil {
		return err
	}
	if len(fixture.Secret) < 32 || len(fixture.Password) < 24 {
		return errors.New("random private fixture credentials required")
	}
	steps := []struct {
		path, key, value string
		fields           map[string]string
	}{
		{"/ip/address", "comment", "md-lab-c", map[string]string{"address": "198.51.100.1/24", "interface": "ether2"}},
		{"/ip/firewall/filter", "comment", "md-lab-traffic-only", map[string]string{"chain": "forward", "in-interface": "ether2", "hotspot": "auth", "dst-address": "10.0.2.2", "protocol": "tcp", "dst-port": "18080", "action": "accept"}},
		{"/ip/firewall/filter", "comment", "md-lab-isolate-host", map[string]string{"chain": "forward", "in-interface": "ether2", "action": "drop"}},
		{"/ip/firewall/nat", "comment", "md-lab-srcnat", map[string]string{"chain": "srcnat", "src-address": "198.51.100.0/24", "out-interface": "ether1", "action": "masquerade"}},
		{"/ip/hotspot/profile", "name", "hsprof-lab", map[string]string{"hotspot-address": "198.51.100.1", "dns-name": "login.example.test", "login-by": "http-pap", "use-radius": "yes", "radius-accounting": "yes", "radius-interim-update": "received"}},
		{"/ip/hotspot", "name", "hs-lab", map[string]string{"interface": "ether2", "profile": "hsprof-lab", "address-pool": "none", "disabled": "no"}},
		{"/radius", "comment", "md-lab-radius", map[string]string{"address": "10.0.2.2", "authentication-port": "12812", "accounting-port": "12813", "service": "hotspot", "secret": fixture.Secret}},
	}
	for _, step := range steps {
		if err := c.ensure(step.path, step.key, step.value, step.fields); err != nil {
			return fmt.Errorf("lab bootstrap %s: %w", step.path, err)
		}
	}
	_, err := c.Do(routeros.Cmd{Path: "/radius/incoming/set", Args: []string{"=accept=yes", "=port=3799"}, Timeout: 5 * time.Second})
	return err
}

// ensure refuses an unexpected existing row. Re-running bootstrap cannot silently
// overwrite a different fixture or undo later probe settings.
func (c *Client) ensure(path, key, value string, fields map[string]string) error {
	keys := []string{".id", key}
	for k := range fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	rows, err := c.Do(routeros.Cmd{Path: path + "/print", Args: []string{"?" + key + "=" + value, "=.proplist=" + strings.Join(keys, ",")}, Timeout: 5 * time.Second})
	if err != nil {
		return errors.New("cannot inspect lab row")
	}
	if len(rows) > 1 {
		return errors.New("ambiguous lab row")
	}
	if len(rows) == 1 {
		for k, v := range fields {
			if rows[0][k] != v {
				return fmt.Errorf("existing lab row differs in %s", k)
			}
		}
		return nil
	}
	args := []string{"=" + key + "=" + value}
	for _, k := range keys {
		if v, ok := fields[k]; ok {
			args = append(args, "="+k+"="+v)
		}
	}
	if _, err := c.Do(routeros.Cmd{Path: path + "/add", Args: args, Timeout: 10 * time.Second}); err != nil {
		return errors.New("lab row creation failed")
	}
	return nil
}

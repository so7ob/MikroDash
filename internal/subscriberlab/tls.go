package subscriberlab

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"time"

	"mikrodash/internal/routeros"
)

// BootstrapHTTPS generates private keys on the disposable NAS. Only public
// certificates leave it; file-upload commands can expose contents in its audit.
func (c *Client) BootstrapHTTPS(privateDir string) error {
	if privateDir != filepath.Dir(c.state.Console) {
		return errors.New("owned private lab directory required")
	}
	for _, cert := range []struct{ name, common, usage, alt, ca string }{
		{"md-lab-ca", "Synthetic CHR Lab CA", "key-cert-sign,crl-sign", "", ""},
		{"md-lab-server", "portal.example.test", "digital-signature,key-agreement,tls-server", "DNS:login.example.test,DNS:portal.example.test", "md-lab-ca"},
	} {
		rows, err := c.Do(routeros.Cmd{Path: "/certificate/print", Args: []string{"?name=" + cert.name, "=.proplist=name,common-name,fingerprint"}, Timeout: 5 * time.Second})
		if err != nil || len(rows) > 1 || len(rows) == 1 && rows[0]["common-name"] != cert.common {
			return errors.New("unexpected disposable certificate identity")
		}
		if len(rows) == 0 {
			args := []string{"=name=" + cert.name, "=common-name=" + cert.common, "=key-size=prime256v1", "=digest-algorithm=sha256", "=days-valid=1", "=key-usage=" + cert.usage}
			if cert.alt != "" {
				args = append(args, "=subject-alt-name="+cert.alt)
			}
			if _, err = c.Do(routeros.Cmd{Path: "/certificate/add", Args: args, Timeout: 5 * time.Second}); err != nil {
				return errors.New("cannot create disposable certificate template")
			}
		}
		if len(rows) == 0 || rows[0]["fingerprint"] == "" {
			// Match the existing API-SSL enrollment path: sign takes singular
			// number, and a completed signature is proven by its fingerprint.
			args := []string{"=number=" + cert.name}
			if cert.ca != "" {
				args = append(args, "=ca="+cert.ca)
			}
			if _, err = c.Do(routeros.Cmd{Path: "/certificate/sign", Args: args, Timeout: 10 * time.Second}); err != nil {
				return errors.New("disposable certificate signing failed; inspect before retry")
			}
			if err = c.awaitLabCertificate(cert.name); err != nil {
				return err
			}
		}
		if _, err = c.Do(routeros.Cmd{Path: "/certificate/set", Args: []string{"=numbers=" + cert.name, "=trusted=yes"}, Timeout: 5 * time.Second}); err != nil {
			return errors.New("cannot trust disposable certificate")
		}
	}
	if err := c.exportPublicCertificate(privateDir, "md-lab-ca"); err != nil {
		return err
	}
	if err := c.exportPublicCertificate(privateDir, "md-lab-server"); err != nil {
		return err
	}
	for _, cmd := range []routeros.Cmd{
		{Path: "/ip/hotspot/profile/set", Args: []string{"=numbers=hsprof-lab", "=login-by=https", "=ssl-certificate=md-lab-server"}, Timeout: 5 * time.Second},
		{Path: "/ip/service/set", Args: []string{"=numbers=reverse-proxy", "=port=9443", "=certificate=md-lab-server", "=disabled=no"}, Timeout: 5 * time.Second},
	} {
		if _, err := c.Do(cmd); err != nil {
			return errors.New("cannot configure disposable HTTPS service")
		}
	}
	if err := c.ensure("/ip/reverse-proxy", "sni", "portal.example.test", map[string]string{"ip-address": "10.0.2.2", "port": "11444", "certificate": "md-lab-server", "disabled": "no"}); err != nil {
		return err
	}
	if err := c.ensure("/ip/hotspot/walled-garden/ip", "comment", "md-lab-portal", map[string]string{"server": "hs-lab", "dst-address": "198.51.100.1", "protocol": "tcp", "dst-port": "9443", "action": "accept"}); err != nil {
		return err
	}
	return c.portalInputRule()
}

func (c *Client) awaitLabCertificate(name string) error {
	for attempt := 0; attempt < 40; attempt++ {
		rows, err := c.Do(routeros.Cmd{Path: "/certificate/print", Args: []string{"?name=" + name, "=.proplist=fingerprint"}, Timeout: time.Second})
		if err != nil {
			return errors.New("cannot observe disposable certificate signature")
		}
		if len(rows) == 1 && rows[0]["fingerprint"] != "" {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("disposable certificate did not finish signing")
}

func (c *Client) exportPublicCertificate(dir, name string) error {
	// No export-passphrase: RouterOS exports only the public CRT, never a key.
	if _, err := c.Do(routeros.Cmd{Path: "/certificate/export-certificate", Args: []string{"=numbers=" + name, "=type=pem", "=file-name=" + name}, Timeout: 5 * time.Second}); err != nil {
		return errors.New("public certificate export failed")
	}
	rows, err := c.Do(routeros.Cmd{Path: "/file/print", Args: []string{"?name=" + name + ".crt", "=.proplist=contents"}, Timeout: 5 * time.Second})
	if err != nil || len(rows) != 1 {
		return errors.New("public certificate unavailable")
	}
	raw := []byte(rows[0]["contents"])
	if err := validatePublicLabCertificate(name, raw); err != nil {
		return err
	}
	path := filepath.Join(dir, name+".crt")
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, raw) {
			return errors.New("refusing to replace a different lab trust anchor")
		}
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create private public-certificate receipt")
	}
	defer f.Close()
	_, err = f.Write(raw)
	return err
}

func validatePublicLabCertificate(name string, raw []byte) error {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("-----BEGIN CERTIFICATE-----")) || len(bytes.TrimSpace(rest)) != 0 {
		return errors.New("refusing non-public certificate material")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || cert.NotAfter.Before(time.Now()) || cert.NotBefore.After(time.Now()) {
		return errors.New("invalid or expired disposable certificate")
	}
	switch name {
	case "md-lab-ca":
		if !cert.IsCA || cert.CheckSignatureFrom(cert) != nil {
			return errors.New("unexpected disposable CA certificate")
		}
	case "md-lab-server":
		if cert.IsCA || cert.VerifyHostname("portal.example.test") != nil || cert.VerifyHostname("login.example.test") != nil {
			return errors.New("unexpected disposable server certificate")
		}
	default:
		return errors.New("unknown disposable certificate")
	}
	return nil
}

func (c *Client) portalInputRule() error {
	rows, err := c.Do(routeros.Cmd{Path: "/ip/firewall/filter/print", Args: []string{"=.proplist=.id,chain,jump-target,comment"}, Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	before := ""
	for _, row := range rows {
		if row["comment"] == "md-lab-portal-input" {
			return c.ensure("/ip/firewall/filter", "comment", "md-lab-portal-input", map[string]string{"chain": "input", "in-interface": "ether2", "protocol": "tcp", "dst-port": "9443", "action": "accept"})
		}
		if row["chain"] == "input" && row["jump-target"] == "hs-input" {
			if before != "" {
				return errors.New("ambiguous lab HotSpot input rule")
			}
			before = row[".id"]
		}
	}
	if before == "" {
		return errors.New("lab HotSpot input rule not proven")
	}
	_, err = c.Do(routeros.Cmd{Path: "/ip/firewall/filter/add", Args: []string{"=chain=input", "=in-interface=ether2", "=protocol=tcp", "=dst-port=9443", "=action=accept", "=comment=md-lab-portal-input", "=place-before=" + before}, Timeout: 5 * time.Second})
	return err
}

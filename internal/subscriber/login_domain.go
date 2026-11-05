package subscriber

import (
	"errors"
	"regexp"
	"strings"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/vendors/microsoft"
	"layeh.com/radius/vendors/mikrotik"
)

var loginDomainSyntax = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,61}[A-Za-z0-9])?$`)

func validLoginDomain(domain string) bool {
	return domain == "" || loginDomainSyntax.MatchString(domain)
}

// HotspotUsername adds only a configured transport selector. Stored account
// names and ticket ownership keep the original username, including Unicode.
func (n NAS) HotspotUsername(username string) (string, error) {
	if !validLoginDomain(n.LoginDomain) {
		return "", errors.New("invalid network login domain")
	}
	if n.LoginDomain == "" {
		return username, nil
	}
	if username == "" || strings.ContainsAny(username, `@\`) {
		return "", errors.New("account name is incompatible with this network login domain; contact support")
	}
	return n.LoginDomain + `\` + username, nil
}

// The NAS must split and normalize the username before the owned RADIUS
// handler sees it. Never strip a browser-supplied prefix in the application.
func (n NAS) matchesLoginDomain(p *radius.Packet) bool {
	if n.LoginDomain == "" {
		return true
	}
	if !validLoginDomain(n.LoginDomain) {
		return false
	}
	users, err := rfc2865.UserName_GetStrings(p)
	if err != nil || len(users) != 1 || users[0] == "" || strings.ContainsAny(users[0], `@\`) {
		return false
	}
	domains, err := microsoft.MSCHAPDomain_GetStrings(p)
	if err != nil || len(domains) != 1 || domains[0] != n.LoginDomain {
		return false
	}
	realms, err := mikrotik.MikrotikRealm_GetStrings(p)
	return err == nil && len(realms) == 1 && realms[0] == n.LoginDomain
}

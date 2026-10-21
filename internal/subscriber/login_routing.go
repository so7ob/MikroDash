package subscriber

import (
	"net"
	"strconv"
	"strings"
	"time"
)

type LoginRoutingTarget struct {
	Address        string
	AuthPort       int
	AccountingPort int
}

type LoginRoutingReview struct {
	ObservedAt           time.Time `json:"observedAt"`
	ConfigurationMatches bool      `json:"configurationMatches"`
	BlockingReasons      []string  `json:"blockingReasons"`
	NamesRequiringReview []string  `json:"namesRequiringReview"`
}

// ReviewLoginRouting compares non-secret metadata, never authorizes admission.
// Protocol capability, local limits and the live pilot need separate acceptance.
func ReviewLoginRouting(r Readiness, n NAS, target LoginRoutingTarget, username string, legacyNames []string, legacyComplete bool) LoginRoutingReview {
	out := LoginRoutingReview{ObservedAt: r.ObservedAt, BlockingReasons: []string{}, NamesRequiringReview: []string{}}
	block := func(reason string) { out.BlockingReasons = append(out.BlockingReasons, reason) }
	if n.LoginDomain == "" || !validLoginDomain(n.LoginDomain) {
		block("A valid explicit login domain is required for this review")
	}
	qualified, err := n.HotspotUsername(username)
	if err != nil || username == "" {
		block("The subscriber name cannot use the configured login domain")
	}
	if net.ParseIP(target.Address) == nil || target.AuthPort < 1 || target.AuthPort > 65535 || target.AccountingPort < 1 || target.AccountingPort > 65535 {
		block("An explicit router-visible RADIUS address and both ports are required")
	}
	if r.Version == "" || r.ObservedAt.IsZero() {
		block("Current router identity and observation time are required")
	}
	for _, menu := range []string{"/ip/hotspot/print", "/ip/hotspot/profile/print", "/radius/print", "/ip/hotspot/user/print"} {
		for _, unavailable := range r.Unavailable {
			if unavailable == menu {
				block("Routing metadata was not freshly read: " + menu)
			}
		}
		if _, present := r.Menus[menu]; !present {
			block("Routing metadata is unavailable: " + menu)
		}
	}
	profileName, servers := "", 0
	for _, row := range r.Menus["/ip/hotspot/print"] {
		if row["name"] == n.CalledStationID {
			servers++
			profileName = row["profile"]
			if row["disabled"] != "false" {
				block("The enrolled HotSpot is disabled or its state is unknown")
			}
		}
	}
	if servers != 1 || profileName == "" {
		block("Exactly one enrolled HotSpot and profile are required")
	}
	profiles := 0
	for _, row := range r.Menus["/ip/hotspot/profile/print"] {
		if row["name"] != profileName {
			continue
		}
		profiles++
		defaultDomain, known := row["radius-default-domain"]
		if !known || defaultDomain != "" || row["split-user-domain"] != "true" || row["use-radius"] != "true" || row["radius-accounting"] != "true" || row["login-by"] != "https" {
			block("The profile requires HTTPS, RADIUS/accounting, splitting and an explicitly empty default domain")
		}
	}
	if profiles != 1 {
		block("Exactly one matching server profile is required")
	}
	selected, eligible := false, false
	for _, row := range r.Menus["/radius/print"] {
		if row["disabled"] == "true" {
			continue
		}
		if row["disabled"] != "false" {
			block("A RADIUS entry has unknown enabled state")
			continue
		}
		if row["service"] == "" {
			block("A RADIUS service list is unknown")
			continue
		}
		services := "," + row["service"] + ","
		if !strings.Contains(services, ",hotspot,") {
			continue
		}
		domain, known := row["domain"]
		if !known {
			block("A HotSpot RADIUS domain is unknown")
			continue
		}
		if domain != "" && domain != n.LoginDomain {
			continue
		}
		eligible = true
		realm, realmKnown := row["realm"]
		selected = domain == n.LoginDomain && realmKnown && (realm == "" || realm == n.LoginDomain) && row["protocol"] == "udp" && row["require-message-auth"] == "yes-for-request-resp" && row["address"] == target.Address && row["authentication-port"] == strconv.Itoa(target.AuthPort) && row["accounting-port"] == strconv.Itoa(target.AccountingPort)
		break // A blank-domain primary first intercepts qualified names on the measured NAS.
	}
	if !eligible || !selected {
		block("The first matching RADIUS entry does not select the intended authenticated domain endpoint")
	}
	for _, row := range r.Menus["/ip/hotspot/user/print"] {
		if row["name"] == "" || row["server"] == "" || row["disabled"] != "true" && row["disabled"] != "false" {
			block("A local HotSpot account has incomplete identity or precedence metadata")
		}
		if row["name"] == username || row["name"] == qualified {
			if row["server"] == "all" || row["server"] == n.CalledStationID || row["server"] == "" {
				if row["disabled"] != "true" {
					block("A local HotSpot account may take precedence over the pilot authority")
				}
			}
		}
	}
	if !legacyComplete {
		block("A complete legacy username inventory is required before enabling splitting")
	}
	for _, name := range legacyNames {
		if name == username || name == qualified {
			block("The pilot username collides with the legacy authority; review failover before admission")
		}
		if strings.ContainsAny(name, `@\`) {
			out.NamesRequiringReview = append(out.NamesRequiringReview, name)
		}
	}
	if len(out.NamesRequiringReview) != 0 {
		block("Legacy names containing domain separators require an explicit compatibility review")
	}
	out.ConfigurationMatches = len(out.BlockingReasons) == 0
	return out
}

package subscriber

import (
	"maps"
	"testing"
	"time"

	"mikrodash/internal/routeros"
)

func routingFixture() (Readiness, NAS, LoginRoutingTarget) {
	r := Readiness{ObservedAt: time.Unix(1792550000, 0), Version: "7.24.5 (stable)", Menus: map[string][]routeros.Reply{
		"/ip/hotspot/print":         {{"name": "hs-C", "profile": "hsprof-C", "disabled": "false"}},
		"/ip/hotspot/profile/print": {{"name": "hsprof-C", "use-radius": "true", "radius-accounting": "true", "login-by": "https", "radius-default-domain": "", "split-user-domain": "true"}},
		"/radius/print":             {{"address": "198.51.100.10", "authentication-port": "11812", "accounting-port": "11813", "domain": "md-pilot", "realm": "", "service": "hotspot", "protocol": "udp", "require-message-auth": "yes-for-request-resp", "disabled": "false"}, {"domain": "", "realm": "", "address": "127.0.0.1", "service": "hotspot", "disabled": "false"}},
		"/ip/hotspot/user/print":    {},
	}}
	return r, NAS{CalledStationID: "hs-C", LoginDomain: "md-pilot"}, LoginRoutingTarget{Address: "198.51.100.10", AuthPort: 11812, AccountingPort: 11813}
}

func TestLoginRoutingReviewRequiresExactMetadataWithoutWriting(t *testing.T) {
	r, n, target := routingFixture()
	got := ReviewLoginRouting(r, n, target, "مشترك تجريبي", []string{"existing-legacy"}, true)
	if !got.ConfigurationMatches || len(got.BlockingReasons) != 0 || got.NamesRequiringReview == nil || got.ObservedAt != r.ObservedAt {
		t.Fatal("valid metadata not recognized", got)
	}
	for _, mode := range []string{"missing-menu", "unavailable-with-stale-rows", "blank-first", "split-off", "default-domain", "unknown-default-domain", "wrong-target", "wrong-auth-port", "wrong-accounting-port", "untrusted-responses", "unknown-domain", "unknown-service", "unknown-enabled", "wrong-realm", "local-precedence", "unknown-local-name", "legacy-unknown", "legacy-separator", "legacy-collision", "no-version", "no-observation"} {
		t.Run(mode, func(t *testing.T) {
			r, n, target := routingFixture()
			legacy := []string{"existing-legacy"}
			complete := true
			switch mode {
			case "unavailable-with-stale-rows":
				r.Unavailable = []string{"/radius/print"}
			case "missing-menu":
				delete(r.Menus, "/ip/hotspot/user/print")
			case "blank-first":
				rows := r.Menus["/radius/print"]
				rows[0], rows[1] = rows[1], rows[0]
			case "split-off":
				r.Menus["/ip/hotspot/profile/print"][0]["split-user-domain"] = "false"
			case "default-domain":
				r.Menus["/ip/hotspot/profile/print"][0]["radius-default-domain"] = "md-pilot"
			case "unknown-default-domain":
				delete(r.Menus["/ip/hotspot/profile/print"][0], "radius-default-domain")
			case "wrong-target":
				target.Address = "198.51.100.11"
			case "wrong-auth-port":
				target.AuthPort = 1812
			case "wrong-accounting-port":
				target.AccountingPort = 1813
			case "untrusted-responses":
				r.Menus["/radius/print"][0]["require-message-auth"] = "no"
			case "unknown-domain":
				delete(r.Menus["/radius/print"][0], "domain")
			case "unknown-service":
				delete(r.Menus["/radius/print"][0], "service")
			case "unknown-enabled":
				delete(r.Menus["/radius/print"][0], "disabled")
			case "wrong-realm":
				r.Menus["/radius/print"][0]["realm"] = "other-authority"
			case "local-precedence":
				r.Menus["/ip/hotspot/user/print"] = []routeros.Reply{{"name": "pilot", "server": "all", "disabled": "false"}}
			case "unknown-local-name":
				r.Menus["/ip/hotspot/user/print"] = []routeros.Reply{{"server": "all", "disabled": "false"}}
			case "legacy-unknown":
				complete = false
			case "legacy-separator":
				legacy = []string{"اسم@قديم"}
			case "legacy-collision":
				legacy = []string{"pilot"}
			case "no-version":
				r.Version = ""
			case "no-observation":
				r.ObservedAt = time.Time{}
			}
			got := ReviewLoginRouting(r, n, target, "pilot", legacy, complete)
			if got.ConfigurationMatches || len(got.BlockingReasons) == 0 {
				t.Fatal("unsafe metadata claimed a match", mode, got)
			}
			if mode == "legacy-separator" && (len(got.NamesRequiringReview) != 1 || got.NamesRequiringReview[0] != "اسم@قديم") {
				t.Fatal("Unicode conflict was translated or lost", got)
			}
		})
	}
}

func TestLoginRoutingReviewDoesNotMutateOrTranslateSourceNames(t *testing.T) {
	r, n, target := routingFixture()
	before := maps.Clone(r.Menus["/radius/print"][0])
	names := []string{"سحب@قديم", `legacy\user`}
	got := ReviewLoginRouting(r, n, target, "pilot", names, true)
	got.NamesRequiringReview[0] = "modified output"
	if names[0] != "سحب@قديم" || !maps.Equal(before, r.Menus["/radius/print"][0]) {
		t.Fatal("routing review mutated original metadata")
	}
}

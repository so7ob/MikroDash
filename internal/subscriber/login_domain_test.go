package subscriber

import (
	"context"
	"strings"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/vendors/microsoft"
	"layeh.com/radius/vendors/mikrotik"
	"mikrodash/internal/routeros"
)

func TestLoginDomainTransportPreservesNamesAndNoDomainCompatibility(t *testing.T) {
	for _, username := range []string{"subscriber", "مشترك سحب", "用户", "legacy@example.test", `legacy\account`} {
		if got, err := (NAS{}).HotspotUsername(username); err != nil || got != username {
			t.Fatal("no-domain compatibility changed", username, got, err)
		}
		got, err := (NAS{LoginDomain: "md-pilot"}).HotspotUsername(username)
		if strings.ContainsAny(username, `@\`) {
			if err == nil || got != "" {
				t.Fatal("ambiguous name rewritten", username, got)
			}
		} else if err != nil || got != `md-pilot\`+username {
			t.Fatal("original Unicode username changed", username, got, err)
		}
	}
	for _, domain := range []string{"@pilot", `pilot\other`, "a b", "https://example.test", "-pilot", "pilot-", "pilot\n", strings.Repeat("a", 64), "مجال"} {
		cfg := landingFixture()
		cfg.Clients[0].LoginDomain = domain
		if err := cfg.Validate(); err == nil {
			t.Fatal("invalid routing domain accepted", domain)
		}
		if _, err := (NAS{LoginDomain: domain}).HotspotUsername("user"); err == nil {
			t.Fatal("invalid transport selector accepted", domain)
		}
	}
}

func TestLoginDomainAuthenticatedUDPRejectsMismatchWithoutConsumingTicket(t *testing.T) {
	s, _, account, address := protocolFixture(t, func(service *RadiusService) {
		r, _, target := routingFixture()
		r.ObservedAt = time.Now()
		r.Menus["/ip/hotspot/print"][0]["name"] = "hs-lab"
		service.Clients[0].LoginDomain = "md-pilot"
		service.Clients[0].LoginRouting = &target
		service.Clients[0].Capabilities = Capabilities{Version: r.Version, Evidence: "synthetic domain routing observation"}
		service.ReadLoginRouting = func(context.Context, string, bool) (LoginRoutingObservation, error) {
			return LoginRoutingObservation{Readiness: r, LegacyNames: []string{}, LegacyComplete: true}, nil
		}
		if err := service.RefreshLoginRouting(t.Context(), "lab"); err != nil {
			t.Fatal(err)
		}
	})
	token, err := s.IssueTicket(t.Context(), TicketRequest{AccountID: account.ID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:01", ClientAddress: "192.0.2.1"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	packet := func(mode string) *radius.Packet {
		p := access(token)
		if mode != "missing" {
			domain, realm := "md-pilot", "md-pilot"
			if mode == "wrong-domain" {
				domain = "another-authority"
			}
			if mode == "wrong-realm" {
				realm = "another-authority"
			}
			if err := microsoft.MSCHAPDomain_SetString(p, domain); err != nil {
				t.Fatal(err)
			}
			if err := mikrotik.MikrotikRealm_SetString(p, realm); err != nil {
				t.Fatal(err)
			}
		}
		switch mode {
		case "duplicate-domain":
			microsoft.MSCHAPDomain_AddString(p, "md-pilot")
		case "duplicate-realm":
			mikrotik.MikrotikRealm_AddString(p, "md-pilot")
		case "duplicate-user":
			rfc2865.UserName_AddString(p, account.Username)
		case "missing-user":
			rfc2865.UserName_Del(p)
		case "unsplit-user":
			rfc2865.UserName_SetString(p, `md-pilot\`+account.Username)
		}
		if err := signResponse(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, mode := range []string{"missing", "wrong-domain", "wrong-realm", "duplicate-domain", "duplicate-realm", "duplicate-user", "missing-user", "unsplit-user"} {
		if got := exchange(t, packet(mode), address); got.Code != radius.CodeAccessReject {
			t.Fatal("unowned domain obtained authorization", mode, got.Code)
		}
	}
	request := packet("valid")
	accepted := exchange(t, request, address)
	if accepted.Code != radius.CodeAccessAccept || rfc2865.Class_GetString(accepted) == "" {
		t.Fatal("valid normalized domain ticket unavailable", accepted.Code)
	}
	if retry := exchange(t, request, address); rfc2865.Class_GetString(retry) != rfc2865.Class_GetString(accepted) {
		t.Fatal("domain retry allocated a second lease")
	}
	if replay := exchange(t, packet("valid"), address); replay.Code != radius.CodeAccessReject {
		t.Fatal("domain ticket reused by another packet")
	}
	if stored, err := s.AccountByID(t.Context(), account.ID); err != nil || stored.Username != account.Username {
		t.Fatal("domain transport rewrote account", stored, err)
	}
}

func FuzzLoginDomainDuplicateIdentityAndVendorBounds(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 1, 55, 10, 2})
	f.Add([]byte{0, 0, 1, 55, 10, 255, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			data = data[:4096]
		}
		p := radius.New(radius.CodeAccessRequest, []byte(testSecret))
		for _, err := range []error{
			rfc2865.UserName_SetString(p, "synthetic-user"),
			microsoft.MSCHAPDomain_SetString(p, "md-pilot"),
			mikrotik.MikrotikRealm_SetString(p, "md-pilot"),
		} {
			if err != nil {
				t.Fatal(err)
			}
		}
		n := NAS{LoginDomain: "md-pilot"}
		if !n.matchesLoginDomain(p) {
			t.Fatal("valid seed identity was not accepted")
		}
		// The mature dictionary must tolerate arbitrary untrusted VSA content.
		p.Add(rfc2865.VendorSpecific_Type, data)
		_ = n.matchesLoginDomain(p)
		// A second username is ambiguous even if malformed or identical.
		p.Add(rfc2865.UserName_Type, data)
		if n.matchesLoginDomain(p) {
			t.Fatal("duplicate identity survived vendor/attribute variation")
		}
	})
}

func TestDomainUDPRequiresLiveRouteBeforeReservation(t *testing.T) {
	r, _, target := routingFixture()
	r.ObservedAt = time.Now()
	r.Menus["/ip/hotspot/print"][0]["name"] = "hs-lab"
	failed := false
	store, service, account, address := protocolFixture(t, func(service *RadiusService) {
		service.Clients[0].LoginDomain = "md-pilot"
		service.Clients[0].LoginRouting = &target
		service.Clients[0].Capabilities = Capabilities{Version: r.Version, Evidence: "synthetic route fixture"}
		service.ReadLoginRouting = func(context.Context, string, bool) (LoginRoutingObservation, error) {
			if failed {
				return LoginRoutingObservation{}, ErrDenied
			}
			return LoginRoutingObservation{Readiness: r, LegacyComplete: true}, nil
		}
	})
	request := func() *radius.Packet {
		p := access("synthetic-password")
		if err := microsoft.MSCHAPDomain_SetString(p, "md-pilot"); err != nil {
			t.Fatal(err)
		}
		if err := mikrotik.MikrotikRealm_SetString(p, "md-pilot"); err != nil {
			t.Fatal(err)
		}
		if err := signResponse(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if got := exchange(t, request(), address); got.Code != radius.CodeAccessReject {
		t.Fatal("cold route allocated an authorization", got.Code)
	}
	r.Menus["/radius/print"] = append(r.Menus["/radius/print"], routeros.Reply{"domain": "", "realm": "", "service": "hotspot", "disabled": "false"})
	if err := service.RefreshLoginRouting(t.Context(), "lab"); err != nil {
		t.Fatal(err)
	}
	if got := exchange(t, request(), address); got.Code != radius.CodeAccessReject {
		t.Fatal("wildcard fallback route allocated an authorization", got.Code)
	}
	if snap, err := store.Snapshot(t.Context(), account.ID, time.Now()); err != nil || len(snap.Sessions) != 0 {
		t.Fatal("refused route reserved credit", snap, err)
	}
	r.Menus["/radius/print"] = r.Menus["/radius/print"][:1]
	if err := service.RefreshLoginRouting(t.Context(), "lab"); err != nil {
		t.Fatal(err)
	}
	if got := exchange(t, request(), address); got.Code != radius.CodeAccessAccept {
		t.Fatal("positive isolated-route control refused", got.Code)
	}
	failed = true
	if err := service.RefreshLoginRouting(t.Context(), "lab"); err == nil {
		t.Fatal("metadata failure was hidden")
	}
	if got := exchange(t, request(), address); got.Code != radius.CodeAccessReject {
		t.Fatal("failed read reused previous permission", got.Code)
	}
	if snap, err := store.Snapshot(t.Context(), account.ID, time.Now()); err != nil || len(snap.Sessions) != 1 {
		t.Fatal("failed route allocated a second lease", snap, err)
	}
}

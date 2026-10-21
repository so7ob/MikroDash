package subscriber

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"net/url"
	"sort"
	"strings"
)

// LandingBundle contains public reviewable assets, never NAS credentials.
// It does not install a directory or change an authentication method.
type LandingBundle struct {
	Network      string
	PortalOrigin string
	Files        map[string][]byte
}

type LandingAsset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

func (b LandingBundle) Manifest() ([]byte, error) {
	names := make([]string, 0, len(b.Files))
	for name := range b.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	assets := make([]LandingAsset, 0, len(names))
	for _, name := range names {
		body := b.Files[name]
		assets = append(assets, LandingAsset{name, Digest(string(body)), len(body)})
	}
	return json.MarshalIndent(struct {
		FormatVersion int            `json:"formatVersion"`
		Network       string         `json:"network"`
		PortalOrigin  string         `json:"portalOrigin"`
		Files         []LandingAsset `json:"files"`
	}{1, b.Network, b.PortalOrigin, assets}, "", "  ")
}

// RenderHotspotLanding uses the documented servlet files and -esc variables.
// Device claims and raw RouterOS errors are deliberately absent from the links.
func RenderHotspotLanding(cfg Config, network string, script []byte) (LandingBundle, error) {
	out := LandingBundle{Network: network, PortalOrigin: cfg.PortalOrigin, Files: map[string][]byte{}}
	if err := cfg.Validate(); err != nil {
		return out, err
	}
	origin, err := url.Parse(cfg.PortalOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || strings.Contains(cfg.PortalOrigin, "$(") {
		return out, errors.New("explicit safe HTTPS portal origin required")
	}
	matches := 0
	for _, n := range cfg.Clients {
		if n.Network == network {
			matches++
		}
	}
	if network == "" || matches != 1 {
		return out, errors.New("select exactly one enrolled network for the HotSpot handoff")
	}
	if len(script) == 0 || len(script) > 4096 {
		return out, errors.New("built HotSpot handoff script must contain 1..4096 bytes")
	}
	query := url.Values{"connect": []string{network}}
	entry := cfg.PortalOrigin + "/?" + query.Encode()
	pages := []struct{ name, mode, title, message string }{
		{"login.html", "login", "Sign in to your account", "Continue to the subscriber portal to sign in and request Internet access."},
		{"rlogin.html", "login", "Sign in to your account", "Continue to the subscriber portal to sign in and request Internet access."},
		{"flogin.html", "failure", "HotSpot sign-in failed", "Your portal account remains available. Return to review the connection request."},
		{"error.html", "failure", "HotSpot could not complete sign-in", "Your portal account remains available. Return to review the connection request."},
		{"alogin.html", "success", "Returning to your account", "The portal will verify this device with the router. Authorization alone does not confirm Internet access."},
		{"status.html", "account", "Open your account", "Open the subscriber portal to review your allowance and devices."},
		{"rstatus.html", "account", "Open your account", "Open the subscriber portal to review your allowance and devices."},
		{"logout.html", "account", "Open your account", "The portal will show the last confirmed Internet session and remaining allowance."},
		{"flogout.html", "account", "Open your account", "The portal will show the last confirmed Internet session and remaining allowance."},
		{"fstatus.html", "account", "Open your account", "Your portal account remains available before Internet sign-in."},
	}
	for _, p := range pages {
		link := cfg.PortalOrigin + "/"
		if p.mode == "login" {
			link = entry
		} else if p.mode == "failure" {
			link += "?hotspot=failed"
		}
		var body bytes.Buffer
		data := struct{ Portal, Link, Mode, Title, Message string }{cfg.PortalOrigin, link, p.mode, p.title, p.message}
		if err = landingTemplate.Execute(&body, data); err != nil {
			return out, err
		}
		if body.Len() > 4096 {
			return out, errors.New("rendered HotSpot page exceeds the small-file readback budget")
		}
		out.Files[p.name] = body.Bytes()
	}
	out.Files["subscriber-handoff.js"] = append([]byte{}, script...)
	// The captive-portal API is intentionally small: RouterOS owns captive state,
	// while all balances and device identity remain in the authenticated portal.
	portalJSON, _ := json.Marshal(entry)
	out.Files["api.json"] = []byte("{\n  \"captive\": $(if logged-in == 'yes')false$(else)true$(endif),\n  \"user-portal-url\": " + string(portalJSON) + "\n}\n")
	return out, nil
}

var landingTemplate = template.Must(template.New("hotspot").Parse(`$(if http-header == "Cache-Control")no-store$(endif)
$(if http-header == "Referrer-Policy")no-referrer$(endif)
$(if http-header == "X-Frame-Options")DENY$(endif)
$(if http-header == "Content-Security-Policy")default-src 'none'; script-src 'self'; style-src {{.Portal}}; img-src {{.Portal}}; font-src {{.Portal}}; connect-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'$(endif)
<!doctype html>
<html lang="en" dir="ltr" data-theme="dark" data-bs-theme="dark">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Subscriber Portal</title>
<link rel="stylesheet" href="{{.Portal}}/vendor/tabler.min.css"><link rel="stylesheet" href="{{.Portal}}/vendor/fonts/fonts.css"><link rel="stylesheet" href="{{.Portal}}/css/app-fonts.css"><link rel="stylesheet" href="{{.Portal}}/app.css"><link rel="stylesheet" href="{{.Portal}}/subscriber.css"><script defer src="subscriber-handoff.js"></script></head>
<body class="subscriber-body" data-portal="{{.Portal}}" data-mode="{{.Mode}}" data-failed="$(if error)yes$(else)no$(endif)" data-return="$(link-redirect-esc)" data-original="$(link-orig-esc)">
<main class="subscriber-shell"><section class="card"><div class="card-header"><img src="{{.Portal}}/logo.png" alt="MikroDash" class="subscriber-logo"><h1 class="card-title">{{.Title}}</h1></div><div class="card-body"><p id="handoffStatus" role="status">{{.Message}}</p><a id="subscriberPortalLink" class="sbtn sbtn-primary" href="{{.Link}}">Continue to subscriber portal</a></div></section></main></body></html>
`))

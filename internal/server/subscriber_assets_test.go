package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubscriberHandoffCanReadPublicFontsWithoutOpeningPrivateCORS(t *testing.T) {
	s, _, cookie, _ := portalFixture(t)
	s.staticDir = t.TempDir()
	if err := os.Mkdir(filepath.Join(s.staticDir, "fonts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.staticDir, "fonts", "synthetic.woff2"), []byte("synthetic font data"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := s.subscriberPortal(t.TempDir())
	for _, tc := range []struct {
		path   string
		status int
		cors   string
	}{
		{"/fonts/synthetic.woff2", 200, "*"},
		{"/api/account", 200, ""},
		{"/api/subscribers", 404, ""},
	} {
		r := httptest.NewRequest("GET", "https://portal.example.test"+tc.path, nil)
		r.Header.Set("Origin", "https://hotspot.example.test")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Access-Control-Allow-Origin") != tc.cors || w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatal("font/private CORS boundary changed", tc.path, w.Code, w.Header())
		}
	}
}

func TestSo7obPortalServesOnlyNamedLocalAssets(t *testing.T) {
	s, _, _, _ := portalFixture(t)
	root, err := filepath.Abs("../../web")
	if err != nil {
		t.Fatal(err)
	}
	s.staticDir = filepath.Join(root, "public")
	handler := s.subscriberPortal(t.TempDir())
	for _, asset := range []string{"subscriber-portal.css", "subscriber/logo-white.svg", "subscriber/tajawal-arabic-400-normal.woff2", "subscriber/tajawal-arabic-700-normal.woff2", "subscriber/tajawal-latin-400-normal.woff2", "subscriber/tajawal-latin-700-normal.woff2", "vendor/jsqr/jsQR.min.js"} {
		r := httptest.NewRequest("GET", "/"+asset, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.Len() == 0 || strings.Contains(w.Header().Get("Content-Security-Policy"), "unsafe-inline") {
			t.Fatal("public asset unavailable or unsafe CSP", asset, w.Code)
		}
		if strings.HasSuffix(asset, ".woff2") && !strings.HasPrefix(w.Body.String(), "wOF2") {
			t.Fatal("invalid bundled WOFF2", asset)
		}
	}
	for _, path := range []string{"/subscriber/", "/subscriber/private.json", "/vendor/jsqr/", "/api/routers", "/app.js"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatal("portal exposed unnamed asset", path, w.Code)
		}
	}
	for _, license := range []struct{ path, text string }{{"subscriber/OFL.txt", "Copyright 2018 Boutros International"}, {"vendor/jsqr/LICENSE", "Apache License"}} {
		data, err := os.ReadFile(filepath.Join(s.staticDir, license.path))
		if err != nil || !strings.Contains(string(data), license.text) {
			t.Fatal("bundled asset license missing", license.path, err)
		}
	}
}

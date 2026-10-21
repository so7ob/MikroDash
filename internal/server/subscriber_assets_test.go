package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
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

package server

import (
	"net/http"
	"path/filepath"
	"strings"
)

// Public licensed font files may be loaded by the NAS handoff page. This
// wrapper is never applied to authenticated routes or administration assets.
func subscriberPublicFonts(directory string) http.Handler {
	files := http.FileServer(http.Dir(directory))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.ToLower(filepath.Ext(r.URL.Path)) {
		case ".woff", ".woff2", ".ttf", ".otf":
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		files.ServeHTTP(w, r)
	})
}

package main

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// webDist holds the built SPA produced by `cd cmd/care/web && npm run build`.
// The pattern is "all:web/dist" so go:embed includes files whose names start
// with "." (Vite emits none today but stay future-proof).
//
//go:embed all:web/dist
var webDist embed.FS

// spaHandler returns an http.Handler that serves the embedded SPA under the
// /ui/ path. Requests for static assets are served directly; everything
// under /ui/<route> that doesn't match a file falls back to index.html so
// React Router can render the route on the client.
//
// Returns false when the embedded dist is empty (e.g. before the SPA was
// built); the caller then logs a hint and skips mounting.
func spaHandler() (http.Handler, bool) {
	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	files := http.FileServer(http.FS(sub))
	stripped := http.StripPrefix("/ui", files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/ui/")
		clean = strings.TrimPrefix(clean, "/")
		if clean == "" || clean == "ui" {
			serveIndex(w, sub)
			return
		}
		if _, err := fs.Stat(sub, clean); err == nil {
			stripped.ServeHTTP(w, r)
			return
		}
		serveIndex(w, sub)
	}), true
}

func serveIndex(w http.ResponseWriter, sub fs.FS) {
	f, err := sub.Open("index.html")
	if err != nil {
		http.Error(w, "index.html missing", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.Copy(w, f)
}

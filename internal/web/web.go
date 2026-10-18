// Package web embeds the built panel (web/ compiled into dist/ by `npm run build`).
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var files embed.FS

const placeholder = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>VPS Admin</title></head>
<body style="margin:0;min-height:100vh;display:grid;place-items:center;background:#141218;color:#E6E0E9;font:16px system-ui,sans-serif">
<p>The panel is not built. Run <code>npm run build</code> in <code>web/</code>, then rebuild the agent.</p></body></html>`

// Handler serves static assets and falls back to index.html for client-side routes.
func Handler() http.Handler {
	dist, _ := fs.Sub(files, "dist")
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(placeholder))
		})
	}
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(dist, p); err != nil {
			// Unknown path without an extension is a panel route.
			if path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		if strings.HasPrefix(p, "assets/") {
			// Vite names assets by content hash.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

package main

import (
	"embed"
	"net/http"
)

//go:embed webadmin/index.html webadmin/app.css webadmin/app.js
var adminWebAssets embed.FS

func webAdminSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
}

func serveWebAdminFile(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		webAdminSecurityHeaders(w)
		data, err := adminWebAssets.ReadFile("webadmin/" + name)
		if err != nil {
			http.Error(w, "admin interface unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

func registerWebAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		webAdminSecurityHeaders(w)
		http.Redirect(w, r, "/admin/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /admin/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/" {
			http.NotFound(w, r)
			return
		}
		serveWebAdminFile("index.html", "text/html; charset=utf-8")(w, r)
	})
	mux.HandleFunc("GET /admin/assets/app.css", serveWebAdminFile("app.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /admin/assets/app.js", serveWebAdminFile("app.js", "text/javascript; charset=utf-8"))
}

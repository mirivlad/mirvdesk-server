package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminWebAssetsAndSecurity(t *testing.T) {
	st := &state{hbbs: &child{}, hbbr: &child{}}
	h := st.handler()
	for _, tc := range []struct{ path, contentType, contains string }{
		{"/admin/", "text/html", "MirvDesk"},
		{"/admin/assets/app.css", "text/css", ".sidebar"},
		{"/admin/assets/app.js", "text/javascript", "sessionStorage"},
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rr.Code != http.StatusOK || !strings.HasPrefix(rr.Header().Get("Content-Type"), tc.contentType) || !strings.Contains(rr.Body.String(), tc.contains) {
			t.Fatalf("%s: HTTP %d / %s", tc.path, rr.Code, rr.Body.String())
		}
		if rr.Header().Get("Content-Security-Policy") == "" || rr.Header().Get("X-Frame-Options") != "DENY" ||
			rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: security headers missing", tc.path)
		}
	}
	for _, path := range []string{"/admin/secrets", "/admin/assets/go.mod", "/admin/assets/"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("unexpected admin route %s: HTTP %d", path, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rr.Code != http.StatusPermanentRedirect || rr.Header().Get("Location") != "/admin/" {
		t.Fatalf("admin redirect: %d %s", rr.Code, rr.Header().Get("Location"))
	}
}

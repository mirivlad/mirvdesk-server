package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	st := &state{hbbs: &child{up: true}, hbbr: &child{up: true}}
	rr := httptest.NewRecorder()
	st.handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected response: %d %s", rr.Code, rr.Body.String())
	}
}

func TestLoginOptions(t *testing.T) {
	st := &state{hbbs: &child{}, hbbr: &child{}}
	rr := httptest.NewRecorder()
	st.handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/login-options", nil))
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("unexpected response: %d %s", rr.Code, rr.Body.String())
	}
}

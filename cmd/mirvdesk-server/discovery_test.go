package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryBehindNginx(t *testing.T) {
	dataDir := t.TempDir()
	rustDir := filepath.Join(dataDir, "rustdesk")
	if err := os.MkdirAll(rustDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rustDir, "id_ed25519.pub"), []byte("public-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIRVDESK_DATA_DIR", dataDir)

	st := &state{hbbs: &child{up: true}, hbbr: &child{up: true}}
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/.well-known/mirvdesk", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Host = "desk.example.com"
	rr := httptest.NewRecorder()
	st.handleDiscovery(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("discovery: %d %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		`"id_server":"desk.example.com:21116"`,
		`"relay_server":"desk.example.com:21117"`,
		`"api_server":"https://desk.example.com"`,
		`"key":"public-key"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
}

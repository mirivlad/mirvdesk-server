package main

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type discoveryResponse struct {
	Schema       int      `json:"schema"`
	IDServer     string   `json:"id_server"`
	RelayServer  string   `json:"relay_server"`
	APIServer    string   `json:"api_server"`
	Key          string   `json:"key"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func firstForwarded(value string) string {
	if i := strings.IndexByte(value, ','); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}
func publicBaseURL(r *http.Request) string {
	if configured := strings.TrimRight(os.Getenv("MIRVDESK_PUBLIC_URL"), "/"); configured != "" {
		return configured
	}
	proto := firstForwarded(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := r.Host
	if host == "" {
		return ""
	}
	return proto + "://" + host
}

func publicHost(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
func (s *state) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	base := publicBaseURL(r)
	host := publicHost(base)
	if host == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "cannot determine public server host")
		return
	}
	idServer := env("MIRVDESK_ID_SERVER", net.JoinHostPort(host, "21116"))
	relayServer := env("MIRVDESK_RELAY_SERVER", net.JoinHostPort(host, "21117"))
	apiServer := env("MIRVDESK_API_PUBLIC_URL", base)
	keyBytes, err := os.ReadFile(filepath.Join(env("MIRVDESK_DATA_DIR", "/data"), "rustdesk", "id_ed25519.pub"))
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "server key is not ready")
		return
	}
	writeJSON(w, http.StatusOK, discoveryResponse{
		Schema:       1,
		IDServer:     idServer,
		RelayServer:  relayServer,
		APIServer:    strings.TrimRight(apiServer, "/"),
		Key:          strings.TrimSpace(string(keyBytes)),
		Version:      version,
		Capabilities: []string{"groups"},
	})
}

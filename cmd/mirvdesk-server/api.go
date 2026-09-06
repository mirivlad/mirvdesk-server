package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type bootstrapRequest struct {
	Token       string `json:"token"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Username   string         `json:"username"`
	Password   string         `json:"password"`
	ID         string         `json:"id"`
	UUID       string         `json:"uuid"`
	AutoLogin  bool           `json:"autoLogin"`
	Type       string         `json:"type"`
	DeviceInfo map[string]any `json:"deviceInfo"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func bearerToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) < 8 || !strings.EqualFold(h[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

func (s *state) handleBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"required": s.store.bootstrapRequired()})
}

func (s *state) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if !s.store.bootstrapRequired() {
		writeAPIError(w, http.StatusConflict, "bootstrap is already complete")
		return
	}
	var req bootstrapRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request")
		return
	}
	u, err := s.store.createFirstAdmin(req.Token, req.Username, req.Password, req.DisplayName)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "user": u})
}

func (s *state) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.store.bootstrapRequired() {
		writeAPIError(w, http.StatusServiceUnavailable, "server setup is required")
		return
	}
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Type != "" && req.Type != "account" {
		writeAPIError(w, http.StatusBadRequest, "unsupported login type")
		return
	}
	u, err := s.store.authenticate(req.Username, req.Password)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, err := s.store.createSession(u.ID, req.ID, req.UUID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"type":         "access_token",
		"access_token": token,
		"user":         u,
	})
}

func (s *state) authenticatedUser(r *http.Request) (user, string, error) {
	token := bearerToken(r)
	if token == "" {
		return user{}, "", errors.New("missing bearer token")
	}
	u, err := s.store.userByToken(token)
	if err != nil {
		return user{}, "", errors.New("invalid or expired session")
	}
	return u, token, nil
}

func (s *state) handleCurrentUser(w http.ResponseWriter, r *http.Request) {
	u, _, err := s.authenticatedUser(r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *state) handleLogout(w http.ResponseWriter, r *http.Request) {
	_, token, err := s.authenticatedUser(r)
	if err == nil {
		_ = s.store.revokeSession(token)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

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

func (s *state) handleLogin(w http.ResponseWriter, r *http.Request) {
	if n, err := s.store.userCount(); err == nil && n == 0 {
		writeAPIError(w, http.StatusServiceUnavailable, "server setup is required; run mirvdesk-admin")
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
	accountKey, sourceKey := loginAttemptKeys(r, req.Username)
	if wait := s.loginLimiter.check(accountKey, sourceKey); wait > 0 {
		writeLoginRateLimit(w, wait)
		return
	}
	u, err := s.store.authenticate(req.Username, req.Password)
	if err != nil {
		s.loginLimiter.failure(accountKey, sourceKey)
		writeAPIError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.loginLimiter.success(accountKey)
	if err := s.store.upsertDevice(u.ID, req.ID, req.UUID, req.DeviceInfo); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to register device")
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

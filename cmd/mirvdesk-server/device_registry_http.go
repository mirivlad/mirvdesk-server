package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
)

const heartbeatWindow = 180 * time.Second

type deviceNonce struct {
	peerID  string
	expires time.Time
}

type deviceHeartbeat struct {
	ID        string `json:"id"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Version   string `json:"version"`
}

func signedHeartbeatMessage(d deviceHeartbeat) []byte {
	return []byte(strings.Join([]string{
		"mirvdesk-heartbeat-v1", d.ID, d.Nonce, d.Hostname, d.OS, d.Arch, d.Version,
	}, "\n"))
}

func validHeartbeatMetadata(d deviceHeartbeat) bool {
	fields := []struct {
		value string
		max   int
	}{
		{d.Hostname, 200}, {d.OS, 40}, {d.Arch, 40}, {d.Version, 80},
	}
	for _, f := range fields {
		if len(f.value) > f.max || strings.ContainsAny(f.value, "\r\n\x00") {
			return false
		}
	}
	return true
}

// Challenge lookup is unauthenticated, but never mutates the registry.
// A nonce is issued only for a key already observed in the local hbbs DB.
func (s *state) handleDeviceChallenge(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !validPeerID(id) {
		writeAPIError(w, 400, "invalid peer ID")
		return
	}
	_, err := s.store.devicePublicKey(id)
	if err != nil && s.rendezvousDataDir != "" {
		_, _ = s.store.syncHBBSDevices(s.rendezvousDataDir)
		_, err = s.store.devicePublicKey(id)
	}
	if err != nil {
		writeAPIError(w, 404, "device has no verified rendezvous key")
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		writeAPIError(w, 500, "nonce generation failed")
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(b)
	now := time.Now()
	s.challengeMu.Lock()
	if s.challenges == nil {
		s.challenges = make(map[string]deviceNonce)
	}
	for k, v := range s.challenges {
		if !v.expires.After(now) {
			delete(s.challenges, k)
		}
	}
	if len(s.challenges) >= 4096 {
		s.challengeMu.Unlock()
		writeAPIError(w, 429, "too many pending device challenges")
		return
	}
	s.challenges[nonce] = deviceNonce{peerID: id, expires: now.Add(60 * time.Second)}
	s.challengeMu.Unlock()
	writeJSON(w, 200, map[string]any{"nonce": nonce, "expires_in": 60})
}

// The detached signature proves possession of the Ed25519 key enrolled in
// hbbs, NOT permission to access the device or to become its account owner.
func (s *state) handleDeviceHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body deviceHeartbeat
	if decodeJSON(r, &body) != nil || !validPeerID(body.ID) ||
		!validHeartbeatMetadata(body) || len(body.Nonce) != 43 {
		writeAPIError(w, 400, "invalid heartbeat")
		return
	}
	s.challengeMu.Lock()
	challenge, found := s.challenges[body.Nonce]
	delete(s.challenges, body.Nonce) // One-time, including on malformed signatures.
	s.challengeMu.Unlock()
	if !found || challenge.peerID != body.ID || !challenge.expires.After(time.Now()) {
		writeAPIError(w, 401, "expired or invalid challenge")
		return
	}
	sig, err := base64.StdEncoding.DecodeString(body.Signature)
	if err != nil { // RustDesk uses unpadded standard base64 for some builds.
		sig, err = base64.RawStdEncoding.DecodeString(body.Signature)
	}
	if err != nil || len(sig) != ed25519.SignatureSize {
		writeAPIError(w, 401, "invalid device signature")
		return
	}
	key, err := s.store.devicePublicKey(body.ID)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(key), signedHeartbeatMessage(body), sig) {
		writeAPIError(w, 401, "device identity verification failed")
		return
	}
	if err := s.store.updateDevicePresence(body.ID, body.Hostname, body.OS, body.Arch, body.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, 404, "unknown device")
			return
		}
		writeAPIError(w, 500, "could not update device presence")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *state) adminSetDeviceDisplayName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if decodeJSON(r, &body) != nil || len(body.Name) > 100 {
		writeAPIError(w, 400, "invalid display name")
		return
	}
	id := r.PathValue("peer")
	if err := s.store.setDeviceDisplayName(id, body.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, 404, "device not found")
			return
		}
		writeAPIError(w, 500, "could not save display name")
		return
	}
	s.recordAdminAudit(r, "device.rename", "device", id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

package main

import (
	"errors"
	"net/http"
	"strings"
)

// These error sentinels keep SQLite implementation errors out of API responses.
var (
	errAdminLastActive = errors.New("cannot disable the last active administrator")
	errUnknownUser     = errors.New("user not found")
)

func (s *store) setUserEnabled(username string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var targetID int64
	var isAdmin, oldStatus int
	err = tx.QueryRow("SELECT id,is_admin,status FROM users WHERE username=?", strings.TrimSpace(username)).
		Scan(&targetID, &isAdmin, &oldStatus)
	if err != nil {
		return errUnknownUser
	}
	if !enabled && isAdmin == 1 && oldStatus == 1 {
		var activeAdmins int
		if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE is_admin=1 AND status=1").Scan(&activeAdmins); err != nil {
			return err
		}
		if activeAdmins <= 1 {
			return errAdminLastActive
		}
	}
	status := 0
	if enabled {
		status = 1
	}
	if _, err := tx.Exec("UPDATE users SET status=? WHERE id=?", status, targetID); err != nil {
		return err
	}
	// Invalidate every session on a status transition; enabling should never
	// resurrect a token minted before the account was disabled.
	if oldStatus != status {
		if _, err := tx.Exec("DELETE FROM sessions WHERE user_id=?", targetID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *state) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid user request")
		return
	}
	// User creation via API always creates an ordinary account. Privilege
	// elevation still requires the operator to use the local admin CLI.
	item, err := s.store.createUser(body.Username, body.Password, body.DisplayName, false)
	if err != nil {
		if strings.Contains(err.Error(), "username") || strings.Contains(err.Error(), "password") {
			writeAPIError(w, http.StatusBadRequest, err.Error())
		} else {
			writeAPIError(w, http.StatusConflict, "username already exists or could not be created")
		}
		return
	}
	s.recordAdminAudit(r, "user.create", "user", item.Name)
	writeJSON(w, http.StatusCreated, item)
}

func (s *state) adminSetUserStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Enabled == nil {
		writeAPIError(w, http.StatusBadRequest, "enabled must be a boolean")
		return
	}
	name := strings.TrimSpace(r.PathValue("username"))
	actor, _, err := s.authenticatedUser(r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !*body.Enabled && strings.EqualFold(name, actor.Name) {
		writeAPIError(w, http.StatusConflict, "cannot disable your own administrator account")
		return
	}
	if err := s.store.setUserEnabled(name, *body.Enabled); err != nil {
		switch {
		case errors.Is(err, errUnknownUser):
			writeAPIError(w, http.StatusNotFound, "user not found")
		case errors.Is(err, errAdminLastActive):
			writeAPIError(w, http.StatusConflict, err.Error())
		default:
			writeAPIError(w, http.StatusInternalServerError, "unable to change user status")
		}
		return
	}
	if *body.Enabled {
		s.recordAdminAudit(r, "user.enable", "user", name)
	} else {
		s.recordAdminAudit(r, "user.disable", "user", name)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *state) adminResetUserPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Password) < 10 {
		writeAPIError(w, http.StatusBadRequest, "password must be at least 10 characters")
		return
	}
	actor, _, err := s.authenticatedUser(r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	name := strings.TrimSpace(r.PathValue("username"))
	if err := s.store.setPassword(name, body.Password); err != nil {
		switch {
		case strings.Contains(err.Error(), "not found"):
			writeAPIError(w, http.StatusNotFound, "user not found")
		default:
			writeAPIError(w, http.StatusInternalServerError, "could not reset password")
		}
		return
	}
	s.recordAdminAuditActor(actor.ID, "user.password_reset", "user", name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

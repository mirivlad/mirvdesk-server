package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type addressBookPutRequest struct {
	Data string `json:"data"`
}

func (s *state) handleAddressBookGet(w http.ResponseWriter, r *http.Request) {
	u, _, err := s.authenticatedUser(r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var data string
	err = s.store.db.QueryRow(`SELECT data FROM address_books WHERE user_id=?`, u.ID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("null\n"))
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load address book")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":             data,
		"licensed_devices": 0,
	})
}

func (s *state) handleAddressBookPut(w http.ResponseWriter, r *http.Request) {
	u, _, err := s.authenticatedUser(r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req addressBookPutRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Data == "" || !json.Valid([]byte(req.Data)) {
		writeAPIError(w, http.StatusBadRequest, "data must be valid JSON")
		return
	}
	_, err = s.store.db.Exec(`INSERT INTO address_books(user_id,data,updated_at)
VALUES(?,?,?)
ON CONFLICT(user_id) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`,
		u.ID, req.Data, time.Now().Unix())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to save address book")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("null\n"))
}

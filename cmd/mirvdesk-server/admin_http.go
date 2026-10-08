package main

import (
	"net/http"
	"strings"
)

// adminOnly always resolves privileges from the session store. A client-side
// is_admin flag must never authorize administrative operations.
func (s *state) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _, err := s.authenticatedUser(r)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !u.IsAdmin {
			writeAPIError(w, http.StatusForbidden, "administrator permission required")
			return
		}
		next(w, r)
	}
}

func (s *state) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/users", s.adminOnly(s.adminListUsers))
	mux.HandleFunc("GET /api/admin/groups", s.adminOnly(s.adminListGroups))
	mux.HandleFunc("POST /api/admin/groups", s.adminOnly(s.adminCreateGroup))
	mux.HandleFunc("POST /api/admin/groups/{group}/members", s.adminOnly(s.adminAddGroupMember))
	mux.HandleFunc("DELETE /api/admin/groups/{group}/members/{username}", s.adminOnly(s.adminRemoveGroupMember))
	mux.HandleFunc("GET /api/admin/devices", s.adminOnly(s.adminListDevices))
	mux.HandleFunc("PUT /api/admin/devices/{peer}/group", s.adminOnly(s.adminSetDeviceGroup))
}

func adminPaginate[T any](w http.ResponseWriter, r *http.Request, items []T) {
	page, pageSize := parsePage(r)
	writeJSON(w, http.StatusOK, map[string]any{"total": len(items), "data": paginate(items, page, pageSize)})
}

func (s *state) adminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.listUsers()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	adminPaginate(w, r, users)
}

func (s *state) adminListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.listDeviceGroups()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}
	adminPaginate(w, r, groups)
}

func (s *state) adminListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.store.listDevices()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list devices")
		return
	}
	adminPaginate(w, r, devices)
}

func (s *state) adminCreateGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid group request")
		return
	}
	name := strings.TrimSpace(body.Name)
	if len(name) == 0 || len(name) > 80 {
		writeAPIError(w, http.StatusBadRequest, "group name must be 1-80 characters")
		return
	}
	if err := s.store.createDeviceGroup(name); err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": strings.TrimSpace(body.Name)})
}

func (s *state) adminAddGroupMember(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid membership request")
		return
	}
	if err := s.store.addUserToDeviceGroup(r.PathValue("group"), body.Username); err != nil {
		writeAPIError(w, http.StatusNotFound, "user or group not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *state) adminRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	if err := s.store.removeUserFromDeviceGroup(r.PathValue("group"), r.PathValue("username")); err != nil {
		writeAPIError(w, http.StatusNotFound, "membership not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *state) adminSetDeviceGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Group string `json:"group"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid group assignment")
		return
	}
	if err := s.store.setDeviceGroup(r.PathValue("peer"), body.Group); err != nil {
		writeAPIError(w, http.StatusNotFound, "device or group not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

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
	mux.HandleFunc("POST /api/admin/users", s.adminOnly(s.adminCreateUser))
	mux.HandleFunc("PATCH /api/admin/users/{username}/status", s.adminOnly(s.adminSetUserStatus))
	mux.HandleFunc("PUT /api/admin/users/{username}/password", s.adminOnly(s.adminResetUserPassword))
	mux.HandleFunc("GET /api/admin/audit", s.adminOnly(s.adminListAudit))
	mux.HandleFunc("GET /api/admin/groups", s.adminOnly(s.adminListGroups))
	mux.HandleFunc("POST /api/admin/groups", s.adminOnly(s.adminCreateGroup))
	mux.HandleFunc("PUT /api/admin/groups/{group}", s.adminOnly(s.adminRenameGroup))
	mux.HandleFunc("DELETE /api/admin/groups/{group}", s.adminOnly(s.adminDeleteGroup))
	mux.HandleFunc("POST /api/admin/groups/{group}/members", s.adminOnly(s.adminAddGroupMember))
	mux.HandleFunc("DELETE /api/admin/groups/{group}/members/{username}", s.adminOnly(s.adminRemoveGroupMember))
	mux.HandleFunc("GET /api/admin/devices", s.adminOnly(s.adminListDevices))
	mux.HandleFunc("PUT /api/admin/devices/{peer}/display-name", s.adminOnly(s.adminSetDeviceDisplayName))
	mux.HandleFunc("PUT /api/admin/devices/{peer}/group", s.adminOnly(s.adminSetDeviceGroup))
	mux.HandleFunc("GET /api/admin/devices/{peer}/groups", s.adminOnly(s.adminGetDeviceGroups))
	mux.HandleFunc("PUT /api/admin/devices/{peer}/note", s.adminOnly(s.adminSetDeviceNote))
	mux.HandleFunc("PUT /api/admin/devices/{peer}/groups", s.adminOnly(s.adminSetDeviceGroups))
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
	u, _, err := s.authenticatedUser(r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "login required")
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope != "" && scope != "all" && scope != "mine" && scope != "accessible" {
		writeAPIError(w, http.StatusBadRequest, "unknown inventory scope")
		return
	}
	devices, err := s.store.listAdminDevicesForAccount(u.ID, scope)
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
	s.recordAdminAudit(r, "group.create", "group", name)
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
	s.recordAdminAudit(r, "group.member.add", "membership", r.PathValue("group")+":"+body.Username)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *state) adminRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	if err := s.store.removeUserFromDeviceGroup(r.PathValue("group"), r.PathValue("username")); err != nil {
		writeAPIError(w, http.StatusNotFound, "membership not found")
		return
	}
	s.recordAdminAudit(r, "group.member.remove", "membership", r.PathValue("group")+":"+r.PathValue("username"))
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
	s.recordAdminAudit(r, "device.group.replace", "device", r.PathValue("peer"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Multi-group API. The singular group endpoint remains for 1.6.x clients.
func (s *state) adminGetDeviceGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.deviceGroupNames(r.PathValue("peer"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list device groups")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (s *state) adminSetDeviceGroups(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Groups []string `json:"groups"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid device group assignment")
		return
	}
	if body.Groups == nil || len(body.Groups) > 20 {
		writeAPIError(w, http.StatusBadRequest, "groups must contain up to 20 group names")
		return
	}
	if err := s.store.setDeviceGroups(r.PathValue("peer"), body.Groups); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.recordAdminAudit(r, "device.groups.replace", "device", r.PathValue("peer"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *state) adminRenameGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid group rename request")
		return
	}
	name := strings.TrimSpace(body.Name)
	if len(name) < 1 || len(name) > 80 {
		writeAPIError(w, http.StatusBadRequest, "group name must be 1-80 characters")
		return
	}
	oldName := r.PathValue("group")
	if err := s.store.renameDeviceGroup(oldName, name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeAPIError(w, http.StatusNotFound, "group not found")
		} else {
			writeAPIError(w, http.StatusConflict, "group name already exists")
		}
		return
	}
	s.recordAdminAudit(r, "group.rename", "group", oldName+" -> "+name)
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (s *state) adminDeleteGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("group")
	if err := s.store.deleteDeviceGroup(name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeAPIError(w, http.StatusNotFound, "group not found")
		} else {
			writeAPIError(w, http.StatusInternalServerError, "failed to delete group")
		}
		return
	}
	s.recordAdminAudit(r, "group.delete", "group", name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *state) adminSetDeviceNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Note string `json:"note"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Note) > 1000 {
		writeAPIError(w, http.StatusBadRequest, "note must be 1000 characters or less")
		return
	}
	id := r.PathValue("peer")
	if err := s.store.setDeviceNote(id, body.Note); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeAPIError(w, http.StatusNotFound, "device not found")
		} else {
			writeAPIError(w, http.StatusInternalServerError, "could not save device note")
		}
		return
	}
	s.recordAdminAudit(r, "device.note.update", "device", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

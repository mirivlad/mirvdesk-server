package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAdminDeviceNotesAndRegistrationTimestamp(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	_, _ = db.createAdmin("admin", "admin-long-password", "Admin")
	_, _ = db.createUser("owner", "owner-long-password", "Owner", false)
	admin := loginForGroups(t, h, "admin", "admin-long-password", "821001", "admin")
	user := loginForGroups(t, h, "owner", "owner-long-password", "821002", "workstation")
	set := request(t, h, http.MethodPut, "/api/admin/devices/821002/note",
		`{"note":"Workshop workstation"}`, admin)
	if set.Code != http.StatusOK {
		t.Fatalf("set note: %d %s", set.Code, set.Body.String())
	}
	list := request(t, h, http.MethodGet, "/api/admin/devices", "", admin)
	if list.Code != http.StatusOK ||
		!strings.Contains(list.Body.String(), "Workshop workstation") ||
		!strings.Contains(list.Body.String(), "last_account_login") {
		t.Fatalf("metadata missing: %s", list.Body.String())
	}
	if denied := request(t, h, http.MethodPut, "/api/admin/devices/821002/note",
		`{"note":"Unauthorized"}`, user); denied.Code != http.StatusForbidden {
		t.Fatalf("nonadmin changed note: %d", denied.Code)
	}
	if long := request(t, h, http.MethodPut, "/api/admin/devices/821002/note",
		fmt.Sprintf("{%q:%q}", "note", strings.Repeat("x", 1001)), admin); long.Code != http.StatusBadRequest {
		t.Fatalf("long note accepted: %d", long.Code)
	}
	if missing := request(t, h, http.MethodPut, "/api/admin/devices/unknown/note",
		`{"note":"test"}`, admin); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown peer accepted: %d", missing.Code)
	}
}

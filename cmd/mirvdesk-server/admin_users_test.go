package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminUserLifecycle(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	if _, err := db.createAdmin("admin", "initial-strong-password", "Admin"); err != nil {
		t.Fatal(err)
	}
	admin := loginForGroups(t, h, "admin", "initial-strong-password", "801001", "AdminPC")
	created := request(t, h, http.MethodPost, "/api/admin/users", `{"username":"alice","password":"initial-user-password","display_name":"Alice"}`, admin)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	token := loginForGroups(t, h, "alice", "initial-user-password", "801002", "Workstation")
	blocked := request(t, h, http.MethodPatch, "/api/admin/users/alice/status", `{"enabled":false}`, admin)
	if blocked.Code != http.StatusOK {
		t.Fatalf("disable: %s", blocked.Body.String())
	}
	who := request(t, h, http.MethodPost, "/api/currentUser", `{}`, token)
	if who.Code != http.StatusUnauthorized {
		t.Fatalf("stale token: %d", who.Code)
	}
	enabled := request(t, h, http.MethodPatch, "/api/admin/users/alice/status", `{"enabled":true}`, admin)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable: %s", enabled.Body.String())
	}
	token = loginForGroups(t, h, "alice", "initial-user-password", "801002", "Workstation")
	reset := request(t, h, http.MethodPut, "/api/admin/users/alice/password", `{"password":"updated-user-password"}`, admin)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset: %s", reset.Body.String())
	}
	who = request(t, h, http.MethodPost, "/api/currentUser", `{}`, token)
	if who.Code != http.StatusUnauthorized {
		t.Fatalf("token after reset: %d", who.Code)
	}
	loginForGroups(t, h, "alice", "updated-user-password", "801002", "Workstation")
	audit := request(t, h, http.MethodGet, "/api/admin/audit", "", admin)
	for _, action := range []string{"user.create", "user.disable", "user.enable", "user.password_reset"} {
		if !strings.Contains(audit.Body.String(), action) {
			t.Fatalf("audit missing %s", action)
		}
	}
}

func TestLastAdministratorCannotBeDisabled(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	if _, err := db.createAdmin("root", "initial-strong-password", "Root"); err != nil {
		t.Fatal(err)
	}
	admin := loginForGroups(t, h, "root", "initial-strong-password", "802001", "RootPC")
	if rr := request(t, h, http.MethodPatch, "/api/admin/users/root/status", `{"enabled":false}`, admin); rr.Code != http.StatusConflict {
		t.Fatalf("self disable: %d", rr.Code)
	}
	if err := db.setUserEnabled("root", false); err != errAdminLastActive {
		t.Fatalf("last admin: %v", err)
	}
}

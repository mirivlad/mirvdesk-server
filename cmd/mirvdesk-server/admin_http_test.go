package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminAPIPrivilegesAndGroupAssignment(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	_, err := db.createAdmin("rootadmin", "admin-long-password", "Operator")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.createUser("alice", "alice-long-password", "Alice", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.createUser("bob", "bob-long-password", "Bob", false)
	if err != nil {
		t.Fatal(err)
	}
	admin := loginForGroups(t, h, "rootadmin", "admin-long-password", "700001", "admin-pc")
	alice := loginForGroups(t, h, "alice", "alice-long-password", "700002", "alice-pc")
	bob := loginForGroups(t, h, "bob", "bob-long-password", "700003", "bob-pc")

	for _, path := range []string{"/api/admin/users", "/api/admin/groups", "/api/admin/devices"} {
		if got := request(t, h, http.MethodGet, path, "", ""); got.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous access to %s: %d", path, got.Code)
		}
		if got := request(t, h, http.MethodGet, path, "", alice); got.Code != http.StatusForbidden {
			t.Fatalf("non-admin access to %s: %d", path, got.Code)
		}
		if got := request(t, h, http.MethodGet, path, "", admin); got.Code != http.StatusOK {
			t.Fatalf("admin access to %s: %d", path, got.Code)
		}
	}
	create := request(t, h, http.MethodPost, "/api/admin/groups", `{"name":"Operations"}`, admin)
	if create.Code != http.StatusCreated {
		t.Fatalf("create group: %d %s", create.Code, create.Body.String())
	}
	if got := request(t, h, http.MethodPost, "/api/admin/groups", `{"name":"No access"}`, alice); got.Code != http.StatusForbidden {
		t.Fatalf("non-admin created group: %d", got.Code)
	}
	set := request(t, h, http.MethodPut, "/api/admin/devices/700002/group", `{"group":"Operations"}`, admin)
	if set.Code != http.StatusOK {
		t.Fatalf("group assignment: %d %s", set.Code, set.Body.String())
	}
	add := request(t, h, http.MethodPost, "/api/admin/groups/Operations/members", `{"username":"bob"}`, admin)
	if add.Code != http.StatusOK {
		t.Fatalf("add group member: %d %s", add.Code, add.Body.String())
	}
	visible := request(t, h, http.MethodGet, "/api/peers", "", bob)
	if visible.Code != http.StatusOK || !strings.Contains(visible.Body.String(), "700002") {
		t.Fatalf("bob should see shared device: %s", visible.Body.String())
	}
	del := request(t, h, http.MethodDelete, "/api/admin/groups/Operations/members/bob", "", admin)
	if del.Code != http.StatusOK {
		t.Fatalf("remove group member: %d %s", del.Code, del.Body.String())
	}
	notVisible := request(t, h, http.MethodGet, "/api/peers", "", bob)
	if strings.Contains(notVisible.Body.String(), "700002") {
		t.Fatalf("bob retained visibility after removal: %s", notVisible.Body.String())
	}
}

func TestAdminAPIRejectsInvalidAssignments(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	_, err := db.createAdmin("admin", "admin-long-password", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	admin := loginForGroups(t, h, "admin", "admin-long-password", "700099", "admin-pc")
	if got := request(t, h, http.MethodPost, "/api/admin/groups", `{"name":""}`, admin); got.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid group rejection, got %d", got.Code)
	}
	if got := request(t, h, http.MethodPut, "/api/admin/devices/700099/group", `{"group":"missing"}`, admin); got.Code != http.StatusNotFound {
		t.Fatalf("expected missing group 404, got %d", got.Code)
	}
}

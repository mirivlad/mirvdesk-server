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

func TestMultipleDeviceGroupsAndLegacyCompatibility(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	_, _ = db.createAdmin("admin", "admin-long-password", "Admin")
	_, _ = db.createUser("owner", "owner-long-password", "Owner", false)
	_, _ = db.createUser("operator", "operator-long-password", "Operator", false)
	_, _ = db.createUser("support", "support-long-password", "Support", false)
	admin := loginForGroups(t, h, "admin", "admin-long-password", "790001", "admin")
	_ = loginForGroups(t, h, "owner", "owner-long-password", "790002", "owner")
	operator := loginForGroups(t, h, "operator", "operator-long-password", "790003", "operator")
	support := loginForGroups(t, h, "support", "support-long-password", "790004", "support")
	for _, name := range []string{"Operations", "Support"} {
		got := request(t, h, http.MethodPost, "/api/admin/groups",
			strings.ReplaceAll(`{"name":"@NAME@"}`, "@NAME@", name), admin)
		if got.Code != http.StatusCreated {
			t.Fatalf("create group %s: %s", name, got.Body.String())
		}
	}
	// This test uses group names from the preceding loop, not its placeholder.
	set := request(t, h, http.MethodPut, "/api/admin/devices/790002/groups",
		`{"groups":["Operations","Support"]}`, admin)
	if set.Code != http.StatusOK {
		t.Fatalf("multi assign: %d %s", set.Code, set.Body.String())
	}
	for _, item := range []struct{ group, user string }{{"Operations", "operator"}, {"Support", "support"}} {
		body := `{"username":"@USER@"}`
		got := request(t, h, http.MethodPost, "/api/admin/groups/"+item.group+"/members",
			strings.ReplaceAll(body, "@USER@", item.user), admin)
		if got.Code != http.StatusOK {
			t.Fatalf("add member: %s", got.Body.String())
		}
	}
	for _, token := range []string{operator, support} {
		visible := request(t, h, http.MethodGet, "/api/peers", "", token)
		if visible.Code != http.StatusOK || !strings.Contains(visible.Body.String(), "790002") {
			t.Fatalf("device should be visible through either group: %s", visible.Body.String())
		}
		if !strings.Contains(visible.Body.String(), "device_group_names") {
			t.Fatalf("new group-name array missing: %s", visible.Body.String())
		}
	}
	groups := request(t, h, http.MethodGet, "/api/admin/devices/790002/groups", "", admin)
	if groups.Code != http.StatusOK || !strings.Contains(groups.Body.String(), "Operations") || !strings.Contains(groups.Body.String(), "Support") {
		t.Fatalf("groups API: %d %s", groups.Code, groups.Body.String())
	}
	// All-or-nothing if one requested group doesn't exist.
	invalid := request(t, h, http.MethodPut, "/api/admin/devices/790002/groups",
		`{"groups":["Operations","MISSING"]}`, admin)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected error: %s", invalid.Body.String())
	}
	names, err := db.deviceGroupNames("790002")
	if err != nil || len(names) != 2 {
		t.Fatalf("partial mutation: %+v: %v", names, err)
	}

	// Singular legacy endpoint replaces the complete set.
	legacy := request(t, h, http.MethodPut, "/api/admin/devices/790002/group",
		`{"group":"Operations"}`, admin)
	if legacy.Code != http.StatusOK {
		t.Fatalf("legacy update failed: %d", legacy.Code)
	}
	hidden := request(t, h, http.MethodGet, "/api/peers", "", support)
	if strings.Contains(hidden.Body.String(), "790002") {
		t.Fatalf("legacy replacement kept stale group visibility: %s", hidden.Body.String())
	}
	names, err = db.deviceGroupNames("790002")
	if err != nil || len(names) != 1 || names[0] != "Operations" {
		t.Fatalf("unexpected group links: %+v: %v", names, err)
	}
}

func TestAdminAuditCapturesMutationsWithoutCredentials(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	_, _ = db.createAdmin("operator", "admin-long-password", "Operator")
	_, _ = db.createUser("employee", "employee-long-password", "Employee", false)
	admin := loginForGroups(t, h, "operator", "admin-long-password", "811001", "operator")
	user := loginForGroups(t, h, "employee", "employee-long-password", "811002", "employee")
	if got := request(t, h, http.MethodPost, "/api/admin/groups",
		`{"name":"Support"}`, admin); got.Code != http.StatusCreated {
		t.Fatalf("failed to create group: %s", got.Body.String())
	}
	if got := request(t, h, http.MethodPut, "/api/admin/devices/811002/groups",
		`{"groups":["Support"]}`, admin); got.Code != http.StatusOK {
		t.Fatalf("failed to assign groups: %s", got.Body.String())
	}
	if got := request(t, h, http.MethodGet, "/api/admin/audit", "", user); got.Code != http.StatusForbidden {
		t.Fatalf("non-admin audit access: %d", got.Code)
	}
	if got := request(t, h, http.MethodGet, "/api/admin/audit", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous audit access: %d", got.Code)
	}
	result := request(t, h, http.MethodGet, "/api/admin/audit", "", admin)
	if result.Code != http.StatusOK ||
		!strings.Contains(result.Body.String(), "device.groups.replace") ||
		!strings.Contains(result.Body.String(), "group.create") ||
		!strings.Contains(result.Body.String(), "operator") {
		t.Fatalf("audit missing entries: %d %s", result.Code, result.Body.String())
	}
	if strings.Contains(result.Body.String(), "admin-long-password") ||
		strings.Contains(result.Body.String(), "employee-long-password") {
		t.Fatalf("audit contains password: %s", result.Body.String())
	}
}

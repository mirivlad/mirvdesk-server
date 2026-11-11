package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminGroupMemberReadPermissionsAndMembership(t *testing.T) {
	st, db := newTestState(t)
	handler := st.handler()
	if _, err := db.createAdmin("operator", "admin-password-long", "Operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.createUser("alice", "user-password-long", "Alice", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.createUser("bob", "user-password-long", "Bob", false); err != nil {
		t.Fatal(err)
	}
	admin := loginForGroups(t, handler, "operator", "admin-password-long", "800001", "admin")
	ordinary := loginForGroups(t, handler, "alice", "user-password-long", "800002", "alice")
	path := "/api/admin/groups/Operations/members"
	if got := request(t, handler, http.MethodGet, path, "", admin); got.Code != http.StatusNotFound {
		t.Fatalf("missing group: %d %s", got.Code, got.Body.String())
	}
	if got := request(t, handler, http.MethodPost, "/api/admin/groups", "{\"name\":\"Operations\"}", admin); got.Code != http.StatusCreated {
		t.Fatalf("group creation: %d %s", got.Code, got.Body.String())
	}
	if got := request(t, handler, http.MethodGet, path, "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous members: %d", got.Code)
	}
	if got := request(t, handler, http.MethodGet, path, "", ordinary); got.Code != http.StatusForbidden {
		t.Fatalf("regular user members: %d", got.Code)
	}
	if got := request(t, handler, http.MethodPost, "/api/admin/groups/Operations/members", "{\"username\":\"bob\"}", admin); got.Code != http.StatusOK {
		t.Fatalf("add member: %s", got.Body.String())
	}
	if got := request(t, handler, http.MethodGet, path, "", admin); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "\"name\":\"bob\"") {
		t.Fatalf("member read failed: %d %s", got.Code, got.Body.String())
	}
}

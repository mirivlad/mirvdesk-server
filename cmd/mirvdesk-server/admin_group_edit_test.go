package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminGroupRenameAndDeleteKeepLinkedDevices(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	_, _ = db.createAdmin("admin", "admin-long-password", "Admin")
	_, _ = db.createUser("alice", "alice-long-password", "Alice", false)
	_, _ = db.createUser("bob", "bob-long-password", "Bob", false)
	admin := loginForGroups(t, h, "admin", "admin-long-password", "713001", "admin")
	_ = loginForGroups(t, h, "alice", "alice-long-password", "713002", "alice")
	bob := loginForGroups(t, h, "bob", "bob-long-password", "713003", "bob")
	for _, name := range []string{"First", "Second"} {
		if err := db.createDeviceGroup(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.setDeviceGroups("713002", []string{"First", "Second"}); err != nil {
		t.Fatal(err)
	}
	if err := db.addUserToDeviceGroup("First", "bob"); err != nil {
		t.Fatal(err)
	}
	if got := request(t, h, http.MethodPut, "/api/admin/groups/First", `{"name":"Primary"}`, admin); got.Code != http.StatusOK {
		t.Fatalf("rename: %s", got.Body.String())
	}
	if seen := request(t, h, http.MethodGet, "/api/peers", "", bob); !strings.Contains(seen.Body.String(), "713002") {
		t.Fatalf("membership broken after rename: %s", seen.Body.String())
	}
	if got := request(t, h, http.MethodDelete, "/api/admin/groups/Primary", "", admin); got.Code != http.StatusOK {
		t.Fatalf("delete: %s", got.Body.String())
	}
	if seen := request(t, h, http.MethodGet, "/api/peers", "", bob); strings.Contains(seen.Body.String(), "713002") {
		t.Fatalf("deleted membership still grants visibility: %s", seen.Body.String())
	}
	groups, err := db.deviceGroupNames("713002")
	if err != nil || len(groups) != 1 || groups[0] != "Second" {
		t.Fatalf("other group lost: %v %v", groups, err)
	}
	peers, err := db.listDevices()
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range peers {
		if peer.ID == "713002" && peer.DeviceGroupName != "Second" {
			t.Fatalf("legacy primary not reassigned: %s", peer.DeviceGroupName)
		}
	}
	if got := request(t, h, http.MethodPut, "/api/admin/groups/Second", `{"name":"Primary"}`, admin); got.Code != http.StatusOK {
		t.Fatalf("rename remaining: %s", got.Body.String())
	}
	if got := request(t, h, http.MethodDelete, "/api/admin/groups/Missing", "", admin); got.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", got.Code)
	}
}

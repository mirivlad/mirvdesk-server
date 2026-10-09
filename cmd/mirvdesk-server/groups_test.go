package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func loginForGroups(t *testing.T, h http.Handler, username, password, id, deviceName string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"username":   username,
		"password":   password,
		"type":       "account",
		"id":         id,
		"uuid":       "uuid-" + id,
		"deviceInfo": map[string]any{"os": "Linux", "device_name": deviceName, "username": username},
	})
	rr := request(t, h, http.MethodPost, "/api/login", string(body), "")
	if rr.Code != http.StatusOK {
		t.Fatalf("login %s: %d %s", username, rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	token, _ := payload["access_token"].(string)
	if token == "" {
		t.Fatalf("login %s returned no token", username)
	}
	return token
}

func TestAccessibleGroupsUsersAndPeers(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	if _, err := db.createAdmin("admin", "admin-strong-password", "Administrator"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.createUser("alice", "alice-strong-password", "Alice", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.createUser("bob", "bob-strong-password", "Bob", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.createUser("carol", "carol-strong-password", "Carol", false); err != nil {
		t.Fatal(err)
	}

	adminToken := loginForGroups(t, h, "admin", "admin-strong-password", "900000001", "admin-pc")
	_ = loginForGroups(t, h, "alice", "alice-strong-password", "900000002", "alice-pc")
	bobToken := loginForGroups(t, h, "bob", "bob-strong-password", "900000003", "bob-pc")
	carolToken := loginForGroups(t, h, "carol", "carol-strong-password", "900000004", "carol-pc")

	if err := db.createDeviceGroup("Operations"); err != nil {
		t.Fatal(err)
	}
	if err := db.setDeviceGroup("900000002", "Operations"); err != nil {
		t.Fatal(err)
	}
	if err := db.addUserToDeviceGroup("Operations", "bob"); err != nil {
		t.Fatal(err)
	}

	rr := request(t, h, http.MethodGet, "/api/device-group/accessible?current=1&pageSize=100", "", bobToken)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"name":"Operations"`) {
		t.Fatalf("bob groups: %d %s", rr.Code, rr.Body.String())
	}
	rr = request(t, h, http.MethodGet, "/api/users?accessible=&status=1&current=1&pageSize=100", "", bobToken)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"name":"alice"`) || !strings.Contains(rr.Body.String(), `"name":"bob"`) {
		t.Fatalf("bob users: %d %s", rr.Code, rr.Body.String())
	}
	rr = request(t, h, http.MethodGet, "/api/peers?accessible=&status=1&current=1&pageSize=100", "", bobToken)
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, `900000002`) || !strings.Contains(body, `900000003`) || strings.Contains(body, `900000004`) {
		t.Fatalf("bob peers: %d %s", rr.Code, body)
	}
	if !strings.Contains(body, `"device_group_name":"Operations"`) {
		t.Fatalf("group name missing from peer payload: %s", body)
	}

	rr = request(t, h, http.MethodGet, "/api/device-group/accessible?current=1&pageSize=100", "", carolToken)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"total":0`) {
		t.Fatalf("carol groups: %d %s", rr.Code, rr.Body.String())
	}
	rr = request(t, h, http.MethodGet, "/api/peers?accessible=&status=1&current=1&pageSize=100", "", carolToken)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `900000004`) || strings.Contains(rr.Body.String(), `900000002`) {
		t.Fatalf("carol peers: %d %s", rr.Code, rr.Body.String())
	}

	rr = request(t, h, http.MethodGet, "/api/peers?accessible=&status=1&current=1&pageSize=2", "", adminToken)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"total":4`) {
		t.Fatalf("admin paged peers: %d %s", rr.Code, rr.Body.String())
	}
}

func TestGroupsAPIRequiresAuthentication(t *testing.T) {
	st, _ := newTestState(t)
	for _, path := range []string{"/api/device-group/accessible", "/api/users", "/api/peers"} {
		rr := request(t, st.handler(), http.MethodGet, path, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s without auth: %d %s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestSameDeviceCanBeUsedByMultipleAccounts(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	if _, err := db.createUser("alice", "alice-password-long", "Alice", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.createUser("bob", "bob-password-long", "Bob", false); err != nil {
		t.Fatal(err)
	}
	loginForGroups(t, h, "alice", "alice-password-long", "900000321", "alice-pc")
	loginForGroups(t, h, "bob", "bob-password-long", "900000321", "bob-pc")
	for _, name := range []string{"alice", "bob"} {
		u, err := db.authenticate(name, name+"-password-long")
		if err != nil {
			t.Fatal(err)
		}
		devices, err := db.accessiblePeers(u)
		if err != nil || len(devices) != 1 || devices[0].ID != "900000321" {
			t.Fatalf("%s device not accessible: %+v; %v", name, devices, err)
		}
		if devices[0].Info["device_name"] != "alice-pc" {
			t.Fatalf("second sign-in overwrote original metadata: %+v", devices[0].Info)
		}
	}
	loginForGroups(t, h, "alice", "alice-password-long", "900000321", "updated-pc")
	alice, _ := db.authenticate("alice", "alice-password-long")
	devices, _ := db.accessiblePeers(alice)
	if devices[0].Info["device_name"] != "updated-pc" {
		t.Fatalf("original record could not be updated: %+v", devices[0].Info)
	}
}

// Restart migration must not re-add a group removed using the new API.
func TestMultiGroupMigrationDoesNotRecreateDeletedLinks(t *testing.T) {
	dir := t.TempDir()
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := db.createUser("owner", "owner-long-password", "Owner", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.upsertDevice(owner.ID, "991001", "uuid-test", nil); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"Legacy", "New"} {
		if err := db.createDeviceGroup(group); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate an old database with devices.group_id and no join-table link.
	if _, err := db.db.Exec(`UPDATE devices SET group_id=(SELECT id FROM device_groups WHERE name='Legacy') WHERE peer_id='991001'`); err != nil {
		t.Fatal(err)
	}
	if err := db.close(); err != nil {
		t.Fatal(err)
	}
	db, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	names, err := db.deviceGroupNames("991001")
	if err != nil || len(names) != 1 || names[0] != "Legacy" {
		t.Fatalf("legacy link not backfilled: %v %v", names, err)
	}
	if err := db.setDeviceGroups("991001", []string{"New"}); err != nil {
		t.Fatal(err)
	}
	if err := db.close(); err != nil {
		t.Fatal(err)
	}
	db, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.close()
	names, err = db.deviceGroupNames("991001")
	if err != nil || len(names) != 1 || names[0] != "New" {
		t.Fatalf("removed legacy group was restored: %v %v", names, err)
	}
}

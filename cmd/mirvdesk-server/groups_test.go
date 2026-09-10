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

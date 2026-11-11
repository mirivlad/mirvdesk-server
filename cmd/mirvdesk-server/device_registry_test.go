package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoveredDeviceRequiresSignedHeartbeat(t *testing.T) {
	state, st := newTestState(t)
	dir := t.TempDir()
	rustdir := filepath.Join(dir, "rustdesk")
	if err := os.MkdirAll(rustdir, 0700); err != nil {
		t.Fatal(err)
	}
	rendezvous, err := sql.Open("sqlite", filepath.Join(rustdir, "db_v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rendezvous.Exec("CREATE TABLE peer(id TEXT NOT NULL, pk BLOB NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rendezvous.Exec("INSERT INTO peer(id,pk) VALUES(?,?)", "987654321", pk); err != nil {
		t.Fatal(err)
	}
	rendezvous.Close()
	state.rendezvousDataDir = dir
	if n, err := st.syncHBBSDevices(dir); err != nil || n != 1 {
		t.Fatalf("discovery: %d, %v", n, err)
	}
	items, err := st.listDevices()
	if err != nil || len(items) != 1 || !items[0].Unassigned || items[0].Presence != "unknown" {
		t.Fatalf("new inventory item: %+v, %v", items, err)
	}
	h := state.handler()
	if res := request(t, h, "POST", "/api/device-registry/heartbeat", "{}", ""); res.Code != 400 {
		t.Fatal("empty proof accepted")
	}
	nonceResponse := request(t, h, "GET", "/api/device-registry/challenge?id=987654321", "", "")
	if nonceResponse.Code != 200 {
		t.Fatalf("nonce: %s", nonceResponse.Body.String())
	}
	var challenge struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(nonceResponse.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	body := deviceHeartbeat{ID: "987654321", Nonce: challenge.Nonce, Hostname: "KASSA-PC", OS: "windows", Arch: "x86_64", Version: "1.7.3"}
	body.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(sk, signedHeartbeatMessage(body)))
	raw, _ := json.Marshal(body)
	if res := request(t, h, "POST", "/api/device-registry/heartbeat", string(raw), ""); res.Code != 200 {
		t.Fatalf("signed heartbeat: %s", res.Body.String())
	}
	if res := request(t, h, "POST", "/api/device-registry/heartbeat", string(raw), ""); res.Code != 401 {
		t.Fatal("replay accepted")
	}
	items, err = st.listDevices()
	if err != nil || len(items) != 1 || !items[0].Online || items[0].Info["name"] != "KASSA-PC" || !items[0].Unassigned {
		t.Fatalf("presence or metadata incorrect: %+v, %v", items, err)
	}
	var memberships int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM user_devices").Scan(&memberships); err != nil || memberships != 0 {
		t.Fatalf("heartbeat assigned MirvDesk ownership: %d %v", memberships, err)
	}

	// A stolen nonce and substituted metadata cannot forge a valid signature.
	response := request(t, h, "GET", "/api/device-registry/challenge?id=987654321", "", "")
	if err := json.Unmarshal(response.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	body.Nonce = challenge.Nonce
	body.Hostname = "VICTIM"
	body.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(sk, signedHeartbeatMessage(body)))
	body.Hostname = "ATTACKER"
	raw, _ = json.Marshal(body)
	if res := request(t, h, "POST", "/api/device-registry/heartbeat", string(raw), ""); res.Code != 401 {
		t.Fatal("modified heartbeat accepted")
	}

	// Key replacement in hbbs cannot take over a pinned device.
	newPK, _, _ := ed25519.GenerateKey(rand.Reader)
	rendezvous, err = sql.Open("sqlite", filepath.Join(rustdir, "db_v2.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rendezvous.Exec("UPDATE peer SET pk=?", newPK); err != nil {
		t.Fatal(err)
	}
	rendezvous.Close()
	if _, err := st.syncHBBSDevices(dir); err != nil {
		t.Fatal(err)
	}
	actualPK, err := st.devicePublicKey("987654321")
	if err != nil || string(actualPK) != string(pk) {
		t.Fatal("pinned identity was replaced")
	}
	if _, err := st.db.Exec("UPDATE devices SET seen_at=? WHERE peer_id=?", time.Now().Add(-10*time.Minute).Unix(), body.ID); err != nil {
		t.Fatal(err)
	}
	items, _ = st.listDevices()
	if items[0].Online || items[0].Presence != "offline" {
		t.Fatalf("presence timeout ignored: %+v", items[0])
	}

	admin, err := st.createAdmin("rootadmin", "long-admin-password", "Root")
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.createSession(admin.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res := request(t, h, "PUT", "/api/admin/devices/987654321/display-name", `{"name":"Касса №2"}`, token); res.Code != 200 {
		t.Fatalf("rename: %s", res.Body.String())
	}
	items, _ = st.listDevices()
	if items[0].DisplayName != "Касса №2" {
		t.Fatal("alias missing")
	}
	if err := st.createDeviceGroup("Бухгалтерия"); err != nil {
		t.Fatal(err)
	}
	if err := st.setDeviceGroups("987654321", []string{"Бухгалтерия"}); err != nil {
		t.Fatal(err)
	}
	other, err := st.createUser("manager", "long-manager-password", "Manager", false)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := st.accessiblePeers(other)
	if err != nil || len(visible) != 0 {
		t.Fatal("group not granted but peer exposed")
	}
	if err := st.addUserToDeviceGroup("Бухгалтерия", "manager"); err != nil {
		t.Fatal(err)
	}
	visible, err = st.accessiblePeers(other)
	if err != nil || len(visible) != 1 {
		t.Fatal("assigned group not visible")
	}
}

func TestLegacyOwnerColumnMigratesWithoutDroppingGroups(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "mirvdesk.db"))
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		"CREATE TABLE users(id INTEGER PRIMARY KEY,username TEXT NOT NULL,password_hash TEXT NOT NULL,display_name TEXT NOT NULL DEFAULT '',is_admin INTEGER NOT NULL DEFAULT 0,status INTEGER NOT NULL DEFAULT 1,created_at INTEGER NOT NULL)",
		"CREATE TABLE device_groups(id INTEGER PRIMARY KEY,name TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL)",
		`CREATE TABLE devices(id INTEGER PRIMARY KEY, peer_id TEXT UNIQUE NOT NULL,
          owner_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          uuid TEXT NOT NULL DEFAULT '', info TEXT NOT NULL DEFAULT '{}',
          note TEXT NOT NULL DEFAULT '',status INTEGER NOT NULL DEFAULT 1,
          group_id INTEGER REFERENCES device_groups(id) ON DELETE SET NULL,
          updated_at INTEGER NOT NULL)`,
		"INSERT INTO users(id,username,password_hash,created_at) VALUES(1,'legacy','hash',1)",
		"INSERT INTO device_groups(id,name,created_at) VALUES(10,'legacy-group',1)",
		"INSERT INTO devices(id,peer_id,owner_user_id,note,group_id,updated_at) VALUES(77,'789',1,'historic note',10,123456)",
	}
	for _, query := range statements {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	items, err := s.listDevices()
	if err != nil || len(items) != 1 || items[0].Note != "historic note" || items[0].DeviceGroupName != "legacy-group" || items[0].Unassigned {
		t.Fatalf("legacy registry migration: %+v %v", items, err)
	}
	// Admin/account deletion preserves the machine as unassigned.
	if _, err := s.db.Exec("DELETE FROM users WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	items, err = s.listDevices()
	if err != nil || len(items) != 1 || !items[0].Unassigned || items[0].DeviceGroupName != "legacy-group" {
		t.Fatalf("delete cascaded to machine or groups: %+v %v", items, err)
	}
	// Migration is idempotent on reopen.
	s.close()
	s, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	items, err = s.listDevices()
	if err != nil || len(items) != 1 {
		t.Fatal("registry lost on reopen")
	}
}

func TestAnonymousRegistryIsNotAnAdminAPI(t *testing.T) {
	state, _ := newTestState(t)
	if result := request(t, state.handler(), "GET", "/api/admin/devices", "", ""); result.Code != http.StatusUnauthorized {
		t.Fatal(result.Code)
	}
	if result := request(t, state.handler(), "GET", "/api/device-registry/challenge?id=unknown", "", ""); result.Code != http.StatusNotFound {
		t.Fatal(result.Code)
	}
	if validPeerID("ID\nATTACK") || validPeerID(strings.Repeat("1", 101)) {
		t.Fatal("invalid rendezvous ID accepted")
	}
}

func TestInventoryScopesRespectAccountLinks(t *testing.T) {
	st, db := newTestState(t)
	h := st.handler()
	admin, _ := db.createAdmin("administrator", "password-long-enough", "Admin")
	_, _ = db.createUser("employee", "employee-long-password", "Employee", false)
	adminToken := loginForGroups(t, h, "administrator", "password-long-enough", "101010", "admin-pc")
	loginForGroups(t, h, "employee", "employee-long-password", "202020", "staff-pc")
	if _, err := db.db.Exec("INSERT INTO devices(peer_id,owner_user_id,info,status,updated_at,first_seen_at) VALUES('303030',NULL,'{}',1,1,1)"); err != nil {
		t.Fatal(err)
	}
	if err := db.createDeviceGroup("Helpdesk"); err != nil {
		t.Fatal(err)
	}
	if err := db.setDeviceGroup("303030", "Helpdesk"); err != nil {
		t.Fatal(err)
	}
	if err := db.addUserToDeviceGroup("Helpdesk", admin.Name); err != nil {
		t.Fatal(err)
	}
	expected := map[string][]string{
		"all":        {"101010", "202020", "303030"},
		"mine":       {"101010"},
		"accessible": {"101010", "303030"},
	}
	for scope, ids := range expected {
		resp := request(t, h, "GET", "/api/admin/devices?scope="+scope, "", adminToken)
		if resp.Code != 200 {
			t.Fatalf("%s: %s", scope, resp.Body.String())
		}
		var page struct {
			Data  []peerPayload `json:"data"`
			Total int           `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != len(ids) {
			t.Fatalf("%s: %+v", scope, page)
		}
		for _, id := range ids {
			if !strings.Contains(resp.Body.String(), id) {
				t.Fatalf("%s missing %s", scope, id)
			}
		}
	}
	bad := request(t, h, "GET", "/api/admin/devices?scope=untrusted", "", adminToken)
	if bad.Code != 400 {
		t.Fatal("invalid admin scope accepted")
	}
	employee, _ := db.authenticate("employee", "employee-long-password")
	employeeToken, _ := db.createSession(employee.ID, "", "")
	rename := request(t, h, "PUT", "/api/admin/devices/303030/display-name", `{"name":"bad"}`, employeeToken)
	if rename.Code != 403 {
		t.Fatal("non-admin renamed device")
	}
}

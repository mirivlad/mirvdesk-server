package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type deviceGroup struct {
	ID   int64  `json:"-"`
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
}

type peerPayload struct {
	ID               string         `json:"id"`
	Info             map[string]any `json:"info"`
	Status           int            `json:"status"`
	User             string         `json:"user"`
	UserName         string         `json:"user_name"`
	DeviceGroupName  string         `json:"device_group_name"`
	DeviceGroupNames []string       `json:"device_group_names,omitempty"`
	Note             string         `json:"note"`
}

func parsePage(r *http.Request) (current, pageSize int) {
	current, _ = strconv.Atoi(r.URL.Query().Get("current"))
	pageSize, _ = strconv.Atoi(r.URL.Query().Get("pageSize"))
	if current < 1 {
		current = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 500 {
		pageSize = 500
	}
	return current, pageSize
}

func paginate[T any](items []T, current, pageSize int) []T {
	start := (current - 1) * pageSize
	if start >= len(items) {
		return []T{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

var errDeviceOwned = errors.New("device is already registered to another account")

func (s *store) upsertDevice(ownerID int64, peerID, uuid string, info map[string]any) error {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return nil
	}
	if info == nil {
		info = map[string]any{}
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`INSERT INTO devices(peer_id,owner_user_id,uuid,info,status,updated_at)
VALUES(?,?,?,?,1,?)
ON CONFLICT(peer_id) DO UPDATE SET uuid=excluded.uuid, info=excluded.info,
status=1, updated_at=excluded.updated_at
WHERE devices.owner_user_id=excluded.owner_user_id`, peerID, ownerID, uuid, string(raw), time.Now().Unix())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errDeviceOwned
	}
	return nil
}

func (s *store) createDeviceGroup(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 80 {
		return errors.New("group name must be 1-80 characters")
	}
	_, err := s.db.Exec(`INSERT INTO device_groups(name,created_at) VALUES(?,?)`, name, time.Now().Unix())
	return err
}

func (s *store) addUserToDeviceGroup(groupName, username string) error {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO device_group_members(group_id,user_id)
SELECT g.id,u.id FROM device_groups g, users u WHERE g.name=? AND u.username=?`, strings.TrimSpace(groupName), strings.TrimSpace(username))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}
	var exists int
	err = s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM device_group_members m JOIN device_groups g ON g.id=m.group_id JOIN users u ON u.id=m.user_id WHERE g.name=? AND u.username=?)`, strings.TrimSpace(groupName), strings.TrimSpace(username)).Scan(&exists)
	if err == nil && exists == 1 {
		return nil
	}
	return errors.New("group or user not found")
}

func (s *store) removeUserFromDeviceGroup(groupName, username string) error {
	res, err := s.db.Exec(`DELETE FROM device_group_members WHERE group_id=(SELECT id FROM device_groups WHERE name=?) AND user_id=(SELECT id FROM users WHERE username=?)`, strings.TrimSpace(groupName), strings.TrimSpace(username))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("membership not found")
	}
	return nil
}

// setDeviceGroup retains the legacy single-group replacement contract.
func (s *store) setDeviceGroup(peerID, groupName string) error {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" || groupName == "-" || strings.EqualFold(groupName, "none") {
		return s.setDeviceGroups(peerID, nil)
	}
	return s.setDeviceGroups(peerID, []string{groupName})
}

// setDeviceGroups changes all links atomically and maintains legacy primary ID.
func (s *store) setDeviceGroups(peerID string, groupNames []string) error {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" || len(groupNames) > 20 {
		return errors.New("invalid device ID or too many groups")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var deviceID int64
	if err := tx.QueryRow("SELECT id FROM devices WHERE peer_id=?", peerID).Scan(&deviceID); err != nil {
		return errors.New("device not found")
	}
	groupIDs := []int64{}
	seen := map[int64]bool{}
	for _, name := range groupNames {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("group name is empty")
		}
		var id int64
		if err := tx.QueryRow("SELECT id FROM device_groups WHERE name=?", name).Scan(&id); err != nil {
			return errors.New("group not found")
		}
		if !seen[id] {
			groupIDs = append(groupIDs, id)
			seen[id] = true
		}
	}
	if _, err := tx.Exec("DELETE FROM device_group_devices WHERE device_id=?", deviceID); err != nil {
		return err
	}
	for _, id := range groupIDs {
		if _, err := tx.Exec("INSERT INTO device_group_devices(group_id,device_id) VALUES(?,?)", id, deviceID); err != nil {
			return err
		}
	}
	var primary any
	if len(groupIDs) > 0 {
		primary = groupIDs[0]
	}
	if _, err := tx.Exec("UPDATE devices SET group_id=? WHERE id=?", primary, deviceID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *store) deviceGroupNames(peerID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT g.name FROM device_group_devices link
JOIN devices d ON d.id=link.device_id
JOIN device_groups g ON g.id=link.group_id
WHERE d.peer_id=? ORDER BY g.name COLLATE NOCASE`, strings.TrimSpace(peerID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *store) listDeviceGroups() ([]deviceGroup, error) {
	rows, err := s.db.Query(`SELECT id,name,note FROM device_groups ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deviceGroup
	for rows.Next() {
		var g deviceGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Note); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *store) listDevices() ([]peerPayload, error) {
	return s.queryPeers(`SELECT d.peer_id,d.info,d.status,u.username,COALESCE(g.name,''),d.note,
COALESCE((SELECT json_group_array(name) FROM (
 SELECT g2.name AS name FROM device_group_devices link2 JOIN device_groups g2 ON g2.id=link2.group_id
 WHERE link2.device_id=d.id ORDER BY g2.name COLLATE NOCASE)), '[]')
FROM devices d JOIN users u ON u.id=d.owner_user_id LEFT JOIN device_groups g ON g.id=d.group_id
ORDER BY u.username COLLATE NOCASE,d.peer_id`)
}

func (s *store) accessibleGroups(u user) ([]deviceGroup, error) {
	query := `SELECT DISTINCT g.id,g.name,g.note FROM device_groups g`
	args := []any{}
	if !u.IsAdmin {
		query += ` LEFT JOIN device_group_members m ON m.group_id=g.id LEFT JOIN device_group_devices link ON link.group_id=g.id LEFT JOIN devices d ON d.id=link.device_id WHERE m.user_id=? OR d.owner_user_id=?`
		args = append(args, u.ID, u.ID)
	}
	query += ` ORDER BY g.name COLLATE NOCASE`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deviceGroup
	for rows.Next() {
		var g deviceGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Note); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *store) accessibleUsers(u user) ([]user, error) {
	if u.IsAdmin {
		users, err := s.listUsers()
		if err != nil {
			return nil, err
		}
		active := users[:0]
		for _, item := range users {
			if item.Status == 1 {
				active = append(active, item)
			}
		}
		return active, nil
	}
	rows, err := s.db.Query(`SELECT DISTINCT u2.id,u2.username,u2.display_name,u2.is_admin,u2.status
FROM users u2
WHERE u2.status=1 AND (u2.id=? OR EXISTS(
  SELECT 1 FROM devices d JOIN device_group_devices link ON link.device_id=d.id JOIN device_group_members m ON m.group_id=link.group_id
  WHERE d.owner_user_id=u2.id AND d.status=1 AND m.user_id=?
)) ORDER BY u2.username COLLATE NOCASE`, u.ID, u.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []user
	for rows.Next() {
		var item user
		var admin int
		if err := rows.Scan(&item.ID, &item.Name, &item.DisplayName, &admin, &item.Status); err != nil {
			return nil, err
		}
		item.IsAdmin = admin != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *store) accessiblePeers(u user) ([]peerPayload, error) {
	base := `SELECT d.peer_id,d.info,d.status,u.username,COALESCE(g.name,''),d.note,
COALESCE((SELECT json_group_array(name) FROM (
 SELECT g2.name AS name FROM device_group_devices link2 JOIN device_groups g2 ON g2.id=link2.group_id
 WHERE link2.device_id=d.id ORDER BY g2.name COLLATE NOCASE)), '[]')
FROM devices d JOIN users u ON u.id=d.owner_user_id LEFT JOIN device_groups g ON g.id=d.group_id`
	if u.IsAdmin {
		return s.queryPeers(base + ` WHERE d.status=1 ORDER BY u.username COLLATE NOCASE,d.peer_id`)
	}
	return s.queryPeers(base+` WHERE d.status=1 AND (d.owner_user_id=? OR EXISTS(SELECT 1 FROM device_group_devices link JOIN device_group_members m ON m.group_id=link.group_id WHERE link.device_id=d.id AND m.user_id=?)) ORDER BY u.username COLLATE NOCASE,d.peer_id`, u.ID, u.ID)
}

func (s *store) queryPeers(query string, args ...any) ([]peerPayload, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []peerPayload
	for rows.Next() {
		var p peerPayload
		var raw string
		var groupNamesJSON string
		if err := rows.Scan(&p.ID, &raw, &p.Status, &p.UserName, &p.DeviceGroupName, &p.Note, &groupNamesJSON); err != nil {
			return nil, err
		}
		p.User = p.UserName
		p.Info = map[string]any{}
		_ = json.Unmarshal([]byte(raw), &p.Info)
		_ = json.Unmarshal([]byte(groupNamesJSON), &p.DeviceGroupNames)
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *state) handleAccessibleDeviceGroups(w http.ResponseWriter, r *http.Request) {
	u, _, err := s.authenticatedUser(r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, err.Error())
		return
	}
	items, err := s.store.accessibleGroups(u)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load device groups")
		return
	}
	current, pageSize := parsePage(r)
	writeJSON(w, http.StatusOK, map[string]any{"total": len(items), "data": paginate(items, current, pageSize)})
}

func (s *state) handleAccessibleUsers(w http.ResponseWriter, r *http.Request) {
	u, _, err := s.authenticatedUser(r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, err.Error())
		return
	}
	items, err := s.store.accessibleUsers(u)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load users")
		return
	}
	current, pageSize := parsePage(r)
	writeJSON(w, http.StatusOK, map[string]any{"total": len(items), "data": paginate(items, current, pageSize)})
}

func (s *state) handleAccessiblePeers(w http.ResponseWriter, r *http.Request) {
	u, _, err := s.authenticatedUser(r)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, err.Error())
		return
	}
	items, err := s.store.accessiblePeers(u)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load peers")
		return
	}
	current, pageSize := parsePage(r)
	writeJSON(w, http.StatusOK, map[string]any{"total": len(items), "data": paginate(items, current, pageSize)})
}

package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// migrateDeviceRegistry preserves legacy IDs and all group/member links. Devices
// discovered by hbbs must not be assigned an artificial MirvDesk account.
func (s *store) migrateDeviceRegistry() error {
	rows, err := s.db.Query("PRAGMA table_info(devices)")
	if err != nil {
		return err
	}
	hasRegistry := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, datatype string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &datatype, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "hbbs_pubkey" {
			hasRegistry = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || hasRegistry {
		return err
	}
	// Foreign-key enforcement must be disabled OUTSIDE the transaction while
	// rebuilding SQLite's legacy NOT NULL owner column.
	if _, err := s.db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer s.db.Exec("PRAGMA foreign_keys=ON")
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE devices_registry_new (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            peer_id TEXT NOT NULL UNIQUE,
            owner_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
            uuid TEXT NOT NULL DEFAULT '',
            info TEXT NOT NULL DEFAULT '{}',
            note TEXT NOT NULL DEFAULT '',
            status INTEGER NOT NULL DEFAULT 1,
            group_id INTEGER REFERENCES device_groups(id) ON DELETE SET NULL,
            updated_at INTEGER NOT NULL,
            display_name TEXT NOT NULL DEFAULT '',
            first_seen_at INTEGER NOT NULL DEFAULT 0,
            seen_at INTEGER NOT NULL DEFAULT 0,
            hbbs_pubkey BLOB
        )`,
		`INSERT INTO devices_registry_new (id,peer_id,owner_user_id,uuid,info,note,status,group_id,updated_at,first_seen_at)
         SELECT id,peer_id,owner_user_id,uuid,info,note,status,group_id,updated_at,updated_at FROM devices`,
		"DROP TABLE devices",
		"ALTER TABLE devices_registry_new RENAME TO devices",
		"CREATE INDEX idx_devices_owner_user_id ON devices(owner_user_id)",
		"CREATE INDEX idx_devices_group_id ON devices(group_id)",
		"CREATE INDEX idx_devices_seen_at ON devices(seen_at)",
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("device registry migration: %w", err)
		}
	}
	fkRows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	hasBrokenReferences := fkRows.Next()
	fkErr := fkRows.Err()
	fkRows.Close()
	if fkErr != nil {
		return fkErr
	}
	if hasBrokenReferences {
		return errors.New("device registry migration would break foreign keys")
	}
	return tx.Commit()
}

func (s *store) syncHBBSDevices(dataDir string) (int, error) {
	// Open the RustDesk OSS rendezvous database read-only. We never alter its
	// peer table or interpret a stored peer row as proof that it is online.
	path := filepath.Join(dataDir, "rustdesk", "db_v2.sqlite3")
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.Query("SELECT id, MIN(pk) FROM peer WHERE length(pk)=32 GROUP BY id HAVING COUNT(DISTINCT hex(pk))=1")
	if err != nil {
		return 0, err
	}
	type entry struct {
		id string
		pk []byte
	}
	entries := make([]entry, 0)
	for rows.Next() {
		var v entry
		if err := rows.Scan(&v.id, &v.pk); err != nil {
			rows.Close()
			return 0, err
		}
		if validPeerID(v.id) {
			entries = append(entries, v)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	count := 0
	for _, peer := range entries {
		// A changed HBBS key never silently replaces an existing device
		// identity. Admin links, notes and group assignments remain intact.
		result, err := s.db.Exec(`INSERT INTO devices(peer_id,owner_user_id,info,status,updated_at,first_seen_at,hbbs_pubkey)
             VALUES(?,NULL,'{}',1,?,?,?)
             ON CONFLICT(peer_id) DO UPDATE SET
               hbbs_pubkey=excluded.hbbs_pubkey
             WHERE devices.hbbs_pubkey IS NULL`,
			peer.id, now, now, peer.pk)
		if err != nil {
			return count, err
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			count++
		}
	}
	return count, nil
}

func validPeerID(id string) bool {
	if len(id) == 0 || len(id) > 100 || strings.TrimSpace(id) != id {
		return false
	}
	for _, c := range id {
		if c < 33 || c > 126 || c == '/' || c == '?' || c == '#' {
			return false
		}
	}
	return true
}

func (s *store) devicePublicKey(id string) ([]byte, error) {
	var pk []byte
	err := s.db.QueryRow("SELECT hbbs_pubkey FROM devices WHERE peer_id=?", id).Scan(&pk)
	if err != nil {
		return nil, err
	}
	if len(pk) != 32 {
		return nil, errors.New("rendezvous public key unavailable")
	}
	return pk, nil
}

func (s *store) updateDevicePresence(id, hostname, osName, arch, clientVersion string) error {
	// No account ownership or group membership is created by a heartbeat.
	// JSON constructed server-side: no arbitrary user-supplied JSON accepted.
	info := map[string]string{"name": hostname, "os": osName, "arch": arch, "version": clientVersion}
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`UPDATE devices SET seen_at=?,info=? WHERE peer_id=? AND hbbs_pubkey IS NOT NULL`,
		time.Now().Unix(), raw, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *store) setDeviceDisplayName(id, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if len(displayName) > 100 {
		return errors.New("display name is too long")
	}
	result, err := s.db.Exec("UPDATE devices SET display_name=? WHERE peer_id=?", displayName, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func logHBBSRegistrySync(s *store, dataDir string) {
	if count, err := s.syncHBBSDevices(dataDir); err != nil {
		log.Printf("hbbs registry sync failed (safe to retry): %v", err)
	} else if count > 0 {
		log.Printf("hbbs registry: %d new or previously unpinned device identities", count)
	}
}

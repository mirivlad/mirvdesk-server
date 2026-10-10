package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

type user struct {
	ID          int64  `json:"-"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar"`
	Email       string `json:"email"`
	Note        string `json:"note"`
	Status      int    `json:"status"`
	IsAdmin     bool   `json:"is_admin"`
}
type store struct {
	db *sql.DB
	mu sync.Mutex
}

func openStore(dataDir string) (*store, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "mirvdesk.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, err
	}
	st := &store{db: db}
	_ = os.Remove(filepath.Join(dataDir, "bootstrap.token"))
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}
func (s *store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  is_admin INTEGER NOT NULL DEFAULT 0,
  status INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash BLOB NOT NULL UNIQUE,
  device_id TEXT NOT NULL DEFAULT '',
  device_uuid TEXT NOT NULL DEFAULT '',
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS address_books (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  data TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS device_groups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  note TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  peer_id TEXT NOT NULL UNIQUE,
  owner_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  uuid TEXT NOT NULL DEFAULT '',
  info TEXT NOT NULL DEFAULT '{}',
  note TEXT NOT NULL DEFAULT '',
  status INTEGER NOT NULL DEFAULT 1,
  group_id INTEGER REFERENCES device_groups(id) ON DELETE SET NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_devices_owner_user_id ON devices(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_devices_group_id ON devices(group_id);
CREATE TABLE IF NOT EXISTS user_devices (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  PRIMARY KEY(user_id,device_id)
);
CREATE INDEX IF NOT EXISTS idx_user_devices_device ON user_devices(device_id);
CREATE TABLE IF NOT EXISTS device_group_members (
  group_id INTEGER NOT NULL REFERENCES device_groups(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY(group_id,user_id)
);
CREATE INDEX IF NOT EXISTS idx_device_group_members_user_id ON device_group_members(user_id);
CREATE TABLE IF NOT EXISTS device_group_devices (
  group_id INTEGER NOT NULL REFERENCES device_groups(id) ON DELETE CASCADE,
  device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  PRIMARY KEY(group_id,device_id)
);
CREATE INDEX IF NOT EXISTS idx_device_group_devices_device_id ON device_group_devices(device_id);
CREATE TABLE IF NOT EXISTS admin_audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_admin_audit_created_at ON admin_audit(created_at);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Existing installations already have device IDs in active/old sessions.
	// Backfill them so upgrading the server does not require every client to log out first.
	_, err := s.db.Exec(`INSERT OR IGNORE INTO devices(peer_id,owner_user_id,uuid,info,status,updated_at)
SELECT device_id,user_id,device_uuid,'{}',1,created_at FROM sessions
WHERE device_id<>'' AND id IN (SELECT MAX(id) FROM sessions WHERE device_id<>'' GROUP BY device_id)`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO user_devices(user_id,device_id)
SELECT owner_user_id,id FROM devices`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO user_devices(user_id,device_id)
SELECT s.user_id,d.id FROM sessions s JOIN devices d ON d.peer_id=s.device_id
WHERE s.device_id<>''`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO device_group_devices(group_id,device_id)
SELECT group_id,id FROM devices WHERE group_id IS NOT NULL`)
	if err != nil {
		return err
	}
	return s.migrateDeviceRegistry()
}
func (s *store) userCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const memory = 64 * 1024
	const iterations = 2
	const parallelism = 1
	const keyLen = 32
	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory, iterations, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return false
	}
	var memory uint32
	var iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func (s *store) createUser(username, password, displayName string, isAdmin bool) (user, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var zero user
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if len(username) < 3 || len(username) > 64 {
		return zero, errors.New("username must be 3-64 characters")
	}
	if len(password) < 10 {
		return zero, errors.New("password must be at least 10 characters")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return zero, err
	}
	admin := 0
	if isAdmin {
		admin = 1
	}
	res, err := s.db.Exec(`INSERT INTO users(username,password_hash,display_name,is_admin,status,created_at)
VALUES(?,?,?,?,?,?)`, username, hash, displayName, admin, 1, time.Now().Unix())
	if err != nil {
		return zero, err
	}
	id, _ := res.LastInsertId()
	return user{ID: id, Name: username, DisplayName: displayName, Status: 1, IsAdmin: isAdmin}, nil
}

func (s *store) createAdmin(username, password, displayName string) (user, error) {
	return s.createUser(username, password, displayName, true)
}

func (s *store) setPassword(username, password string) error {
	username = strings.TrimSpace(username)
	if len(password) < 10 {
		return errors.New("password must be at least 10 characters")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE users SET password_hash=? WHERE username=?`, hash, username)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("user not found")
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=(SELECT id FROM users WHERE username=?)`, username); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *store) listUsers() ([]user, error) {
	rows, err := s.db.Query(`SELECT id,username,display_name,is_admin,status FROM users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []user
	for rows.Next() {
		var u user
		var admin int
		if err := rows.Scan(&u.ID, &u.Name, &u.DisplayName, &admin, &u.Status); err != nil {
			return nil, err
		}
		u.IsAdmin = admin != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *store) authenticate(username, password string) (user, error) {
	var u user
	var hash string
	var admin int
	err := s.db.QueryRow(`SELECT id,username,password_hash,display_name,is_admin,status
FROM users WHERE username=?`, strings.TrimSpace(username)).Scan(
		&u.ID, &u.Name, &hash, &u.DisplayName, &admin, &u.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, errors.New("invalid username or password")
		}
		return u, err
	}
	if u.Status != 1 || !verifyPassword(hash, password) {
		return user{}, errors.New("invalid username or password")
	}
	u.IsAdmin = admin != 0
	return u, nil
}

func newSessionToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(raw))
	return raw, h[:], nil
}
func (s *store) createSession(userID int64, deviceID, deviceUUID string) (string, error) {
	raw, hash, err := newSessionToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	expires := now.Add(30 * 24 * time.Hour)
	_, err = s.db.Exec(`INSERT INTO sessions(user_id,token_hash,device_id,device_uuid,expires_at,created_at)
VALUES(?,?,?,?,?,?)`, userID, hash, deviceID, deviceUUID, expires.Unix(), now.Unix())
	if err != nil {
		return "", err
	}
	return raw, nil
}

func (s *store) userByToken(raw string) (user, error) {
	var u user
	var admin int
	h := sha256.Sum256([]byte(raw))
	err := s.db.QueryRow(`SELECT u.id,u.username,u.display_name,u.is_admin,u.status
FROM sessions s JOIN users u ON u.id=s.user_id
WHERE s.token_hash=? AND s.expires_at>? AND u.status=1`, h[:], time.Now().Unix()).Scan(
		&u.ID, &u.Name, &u.DisplayName, &admin, &u.Status)
	if err != nil {
		return u, err
	}
	u.IsAdmin = admin != 0
	return u, nil
}
func (s *store) revokeSession(raw string) error {
	h := sha256.Sum256([]byte(raw))
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, h[:])
	return err
}

func (s *store) close() error {
	return s.db.Close()
}

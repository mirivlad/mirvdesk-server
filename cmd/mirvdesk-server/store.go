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
	db             *sql.DB
	mu             sync.Mutex
	bootstrapToken string
	bootstrapPath  string
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
	st := &store{db: db, bootstrapPath: filepath.Join(dataDir, "bootstrap.token")}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := st.ensureBootstrapToken(); err != nil {
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
`
	_, err := s.db.Exec(schema)
	return err
}
func (s *store) userCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *store) ensureBootstrapToken() error {
	n, err := s.userCount()
	if err != nil {
		return err
	}
	if n > 0 {
		_ = os.Remove(s.bootstrapPath)
		return nil
	}
	if b, err := os.ReadFile(s.bootstrapPath); err == nil {
		s.bootstrapToken = strings.TrimSpace(string(b))
		if s.bootstrapToken != "" {
			return nil
		}
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	s.bootstrapToken = base64.RawURLEncoding.EncodeToString(b)
	return os.WriteFile(s.bootstrapPath, []byte(s.bootstrapToken+"\n"), 0600)
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

func (s *store) createFirstAdmin(token, username, password, displayName string) (user, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var zero user
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.bootstrapToken)) != 1 {
		return zero, errors.New("invalid bootstrap token")
	}
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if len(username) < 3 || len(username) > 64 {
		return zero, errors.New("username must be 3-64 characters")
	}
	if len(password) < 10 {
		return zero, errors.New("password must be at least 10 characters")
	}
	n, err := s.userCount()
	if err != nil || n != 0 {
		return zero, errors.New("bootstrap is already complete")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return zero, err
	}
	res, err := s.db.Exec(`INSERT INTO users(username,password_hash,display_name,is_admin,status,created_at)
VALUES(?,?,?,?,?,?)`, username, hash, displayName, 1, 1, time.Now().Unix())
	if err != nil {
		return zero, err
	}
	id, _ := res.LastInsertId()
	s.bootstrapToken = ""
	_ = os.Remove(s.bootstrapPath)
	return user{ID: id, Name: username, DisplayName: displayName, Status: 1, IsAdmin: true}, nil
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
WHERE s.token_hash=? AND s.expires_at>?`, h[:], time.Now().Unix()).Scan(
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

func (s *store) bootstrapRequired() bool {
	n, err := s.userCount()
	return err == nil && n == 0
}

func (s *store) close() error {
	return s.db.Close()
}

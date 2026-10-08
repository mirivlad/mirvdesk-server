package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestState(t *testing.T) (*state, *store) {
	t.Helper()
	stg, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stg.close() })
	return &state{hbbs: &child{up: true}, hbbr: &child{up: true}, store: stg}, stg
}

func request(t *testing.T, h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}
func TestLoginCurrentUserLogout(t *testing.T) {
	st, stg := newTestState(t)
	h := st.handler()
	if _, err := stg.createAdmin("vladimir", "very-strong-password", "Vladimir"); err != nil {
		t.Fatal(err)
	}
	loginBody := `{"username":"vladimir","password":"very-strong-password","type":"account","id":"123","uuid":"abc","deviceInfo":{"os":"linux"}}`
	rr := request(t, h, http.MethodPost, "/api/login", loginBody, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rr.Code, rr.Body.String())
	}
	var login map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	token, _ := login["access_token"].(string)
	if token == "" || login["type"] != "access_token" {
		t.Fatalf("bad login response: %s", rr.Body.String())
	}
	rr = request(t, h, http.MethodPost, "/api/currentUser", `{"id":"123","uuid":"abc"}`, token)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"name":"vladimir"`) {
		t.Fatalf("current user: %d %s", rr.Code, rr.Body.String())
	}
	rr = request(t, h, http.MethodPost, "/api/logout", `{"id":"123","uuid":"abc"}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("logout: %d %s", rr.Code, rr.Body.String())
	}
	rr = request(t, h, http.MethodPost, "/api/currentUser", `{}`, token)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("token still valid after logout: %d %s", rr.Code, rr.Body.String())
	}
}

func TestLoginRequiresLocalSetup(t *testing.T) {
	st, _ := newTestState(t)
	rr := request(t, st.handler(), http.MethodPost, "/api/login",
		`{"username":"admin","password":"anything","type":"account","deviceInfo":{}}`, "")
	if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "mirvdesk-admin") {
		t.Fatalf("unexpected setup response: %d %s", rr.Code, rr.Body.String())
	}
	for _, path := range []string{"/api/bootstrap", "/api/bootstrap/status"} {
		rr = request(t, st.handler(), http.MethodPost, path, `{}`, "")
		if rr.Code != http.StatusNotFound {
			t.Fatalf("legacy bootstrap endpoint %s is still exposed: %d", path, rr.Code)
		}
	}
}
func TestBadPasswordRejected(t *testing.T) {
	st, stg := newTestState(t)
	if _, err := stg.createAdmin("admin", "very-strong-password", ""); err != nil {
		t.Fatal(err)
	}
	rr := request(t, st.handler(), http.MethodPost, "/api/login",
		`{"username":"admin","password":"wrong-password","type":"account","deviceInfo":{}}`, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected response: %d %s", rr.Code, rr.Body.String())
	}
}

func TestLegacyAddressBookRoundTrip(t *testing.T) {
	st, stg := newTestState(t)
	if _, err := stg.createAdmin("admin", "very-strong-password", ""); err != nil {
		t.Fatal(err)
	}
	login := request(t, st.handler(), http.MethodPost, "/api/login",
		`{"username":"admin","password":"very-strong-password","type":"account","deviceInfo":{}}`, "")
	var payload map[string]any
	if err := json.Unmarshal(login.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	token, _ := payload["access_token"].(string)
	if token == "" {
		t.Fatal("missing access token")
	}
	rr := request(t, st.handler(), http.MethodGet, "/api/ab", "", token)
	if rr.Code != http.StatusOK || rr.Body.String() != "null" {
		t.Fatalf("initial address book: %d %s", rr.Code, rr.Body.String())
	}
	body := `{"data":"{\"tags\":[\"prod\"],\"peers\":[{\"id\":\"123456789\",\"alias\":\"server\"}]}"}`
	rr = request(t, st.handler(), http.MethodPost, "/api/ab", body, token)
	if rr.Code != http.StatusOK || rr.Body.String() != "null" {
		t.Fatalf("save address book: %d %s", rr.Code, rr.Body.String())
	}
	rr = request(t, st.handler(), http.MethodGet, "/api/ab", "", token)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `123456789`) {
		t.Fatalf("load address book: %d %s", rr.Code, rr.Body.String())
	}
	unauthorized := request(t, st.handler(), http.MethodGet, "/api/ab", "", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized address book: %d %s", unauthorized.Code, unauthorized.Body.String())
	}
}
func TestPasswordResetRevokesSessions(t *testing.T) {
	st, stg := newTestState(t)
	if _, err := stg.createAdmin("admin", "old-strong-password", ""); err != nil {
		t.Fatal(err)
	}
	login := request(t, st.handler(), http.MethodPost, "/api/login",
		`{"username":"admin","password":"old-strong-password","type":"account","deviceInfo":{}}`, "")
	var payload map[string]any
	if err := json.Unmarshal(login.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	token, _ := payload["access_token"].(string)
	if token == "" {
		t.Fatal("missing access token")
	}
	if err := stg.setPassword("admin", "new-strong-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := stg.userByToken(token); err == nil {
		t.Fatal("old session survived password reset")
	}
	if _, err := stg.authenticate("admin", "old-strong-password"); err == nil {
		t.Fatal("old password still works")
	}
	if _, err := stg.authenticate("admin", "new-strong-password"); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
}

func TestRevokedUserCannotReuseActiveToken(t *testing.T) {
	st, db := newTestState(t)
	u, err := db.createUser("alice", "alice-password-long", "Alice", false)
	if err != nil {
		t.Fatal(err)
	}
	token, err := db.createSession(u.ID, "test-device", "test-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.userByToken(token); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("UPDATE users SET status=0 WHERE id=?", u.ID); err != nil {
		t.Fatal(err)
	}
	got := request(t, st.handler(), http.MethodPost, "/api/currentUser", "{}", token)
	if got.Code != http.StatusUnauthorized {
		t.Fatalf("disabled account had valid token: %d", got.Code)
	}
}

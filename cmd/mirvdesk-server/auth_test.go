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
func TestBootstrapLoginCurrentUserLogout(t *testing.T) {
	st, stg := newTestState(t)
	h := st.handler()

	body := `{"token":"` + stg.bootstrapToken + `","username":"vladimir","password":"very-strong-password","display_name":"Vladimir"}`
	rr := request(t, h, http.MethodPost, "/api/bootstrap", body, "")
	if rr.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", rr.Code, rr.Body.String())
	}
	if stg.bootstrapRequired() {
		t.Fatal("bootstrap should be complete")
	}

	loginBody := `{"username":"vladimir","password":"very-strong-password","type":"account","id":"123","uuid":"abc","deviceInfo":{"os":"linux"}}`
	rr = request(t, h, http.MethodPost, "/api/login", loginBody, "")
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

func TestBadPasswordRejected(t *testing.T) {
	st, stg := newTestState(t)
	if _, err := stg.createFirstAdmin(stg.bootstrapToken, "admin", "very-strong-password", ""); err != nil {
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
	if _, err := stg.createFirstAdmin(stg.bootstrapToken, "admin", "very-strong-password", ""); err != nil {
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
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "null" {
		t.Fatalf("initial address book: %d %s", rr.Code, rr.Body.String())
	}

	body := `{"data":"{\"tags\":[\"prod\"],\"peers\":[{\"id\":\"123456789\",\"alias\":\"server\"}]}"}`
	rr = request(t, st.handler(), http.MethodPost, "/api/ab", body, token)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "null" {
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

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginLimiterBlocksAndRecovers(t *testing.T) {
	st, db := newTestState(t)
	_, err := db.createAdmin("admin", "very-strong-password", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	current := time.Now()
	st.loginLimiter.now = func() time.Time { return current }
	req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	account, source := loginAttemptKeys(req, "admin")
	for i := 0; i < loginUserFailureLimit; i++ {
		st.loginLimiter.failure(account, source)
	}
	body := `{"username":"admin","password":"very-strong-password","type":"account","deviceInfo":{}}`
	blocked := request(t, st.handler(), http.MethodPost, "/api/login", body, "")
	if blocked.Code != http.StatusTooManyRequests || blocked.Header().Get("Retry-After") == "" {
		t.Fatalf("expected 429 with Retry-After, got %d: %s", blocked.Code, blocked.Body.String())
	}
	current = current.Add(loginLockout + time.Second)
	ok := request(t, st.handler(), http.MethodPost, "/api/login", body, "")
	if ok.Code != http.StatusOK {
		t.Fatalf("login did not recover after cooldown: %d %s", ok.Code, ok.Body.String())
	}
	if st.loginLimiter.check(account, source) > 0 {
		t.Fatal("successful authentication did not clear account lockout")
	}
}

func TestLoginLimiterPerSourcePreventsUsernameSpray(t *testing.T) {
	l := &loginAttemptLimiter{}
	req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	for i := 0; i < loginIPFailureLimit; i++ {
		acct, source := loginAttemptKeys(req, string(rune('a'+i)))
		l.failure(acct, source)
	}
	nextAccount, source := loginAttemptKeys(req, "new-user")
	if l.check(nextAccount, source) <= 0 {
		t.Fatal("IP-wide limiter did not block username spraying")
	}
}

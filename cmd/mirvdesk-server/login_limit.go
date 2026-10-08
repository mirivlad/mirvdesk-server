package main

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	loginFailureWindow    = 10 * time.Minute
	loginLockout          = 2 * time.Minute
	loginUserFailureLimit = 8
	loginIPFailureLimit   = 80
	loginAttemptMapLimit  = 4096
)

type loginAttempt struct {
	first        time.Time
	failures     int
	blockedUntil time.Time
}

// A bounded in-memory limiter protects the password API without trusting
// spoofable X-Forwarded-For headers. The reverse proxy may be 127.0.0.1, so
// per-username limits remain effective even behind a local proxy.
type loginAttemptLimiter struct {
	mu      sync.Mutex
	records map[string]loginAttempt
	now     func() time.Time
}

func (l *loginAttemptLimiter) nowTime() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func loginAttemptKeys(r *http.Request, username string) (account, source string) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	username = strings.ToLower(strings.TrimSpace(username))
	if len(username) > 256 {
		username = username[:256]
	}
	if ip == "" {
		ip = "unknown"
	}
	return "account:" + username + ":" + ip, "ip:" + ip
}

func (l *loginAttemptLimiter) check(account, source string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.nowTime()
	var wait time.Duration
	for _, key := range []string{account, source} {
		record := l.records[key]
		if waitFor := record.blockedUntil.Sub(now); waitFor > wait {
			wait = waitFor
		}
	}
	return wait
}

func (l *loginAttemptLimiter) failure(account, source string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.records == nil {
		l.records = make(map[string]loginAttempt)
	}
	now := l.nowTime()
	// Keep memory bounded under username-spray attempts.
	if len(l.records) >= loginAttemptMapLimit {
		for key, record := range l.records {
			if now.Sub(record.first) > loginFailureWindow && !now.Before(record.blockedUntil) {
				delete(l.records, key)
			}
		}
	}
	for _, bucket := range []struct {
		key   string
		limit int
	}{
		{account, loginUserFailureLimit}, {source, loginIPFailureLimit},
	} {
		entry, exists := l.records[bucket.key]
		if !exists && len(l.records) >= loginAttemptMapLimit {
			continue
		}
		if now.Sub(entry.first) > loginFailureWindow ||
			(!entry.blockedUntil.IsZero() && !now.Before(entry.blockedUntil)) {
			entry = loginAttempt{first: now}
		}
		entry.failures++
		if entry.failures >= bucket.limit {
			entry.blockedUntil = now.Add(loginLockout)
		}
		l.records[bucket.key] = entry
	}
}

func (l *loginAttemptLimiter) success(account string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.records, account)
}

func writeLoginRateLimit(w http.ResponseWriter, wait time.Duration) {
	seconds := int(wait.Seconds()) + 1
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeAPIError(w, http.StatusTooManyRequests, "too many login attempts; retry later")
}

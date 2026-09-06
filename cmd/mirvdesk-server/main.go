package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var (
	version               = "dev"
	rustDeskServerVersion = "unknown"
)

type child struct {
	name string
	path string
	dir  string
	mu   sync.RWMutex
	cmd  *exec.Cmd
	up   bool
}

func (c *child) start() (<-chan error, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cmd = exec.Command(c.path)
	c.cmd.Dir = c.dir
	c.cmd.Stdout = os.Stdout
	c.cmd.Stderr = os.Stderr
	c.cmd.Env = os.Environ()
	if err := c.cmd.Start(); err != nil {
		return nil, err
	}
	c.up = true
	done := make(chan error, 1)
	go func() {
		err := c.cmd.Wait()
		c.mu.Lock()
		c.up = false
		c.mu.Unlock()
		done <- err
	}()
	return done, nil
}
func (c *child) running() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.up
}

func (c *child) stop() {
	c.mu.RLock()
	cmd, up := c.cmd, c.up
	c.mu.RUnlock()
	if up && cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
}

type state struct {
	hbbs  *child
	hbbr  *child
	store *store
}

func (s *state) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ok := s.hbbs.running() && s.hbbr.running()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   ok,
			"hbbs": s.hbbs.running(),
			"hbbr": s.hbbr.running(),
		})
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"name":            "mirvdesk-server",
			"version":         version,
			"rustdesk_server": rustDeskServerVersion,
		})
	})
	mux.HandleFunc("GET /.well-known/mirvdesk", s.handleDiscovery)
	mux.HandleFunc("GET /api/login-options", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]\n"))
	})
	mux.HandleFunc("GET /api/bootstrap/status", s.handleBootstrapStatus)
	mux.HandleFunc("POST /api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/currentUser", s.handleCurrentUser)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/ab", s.handleAddressBookGet)
	mux.HandleFunc("POST /api/ab", s.handleAddressBookPut)
	return mux
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	syscall.Umask(0077)
	dataDir := env("MIRVDESK_DATA_DIR", "/data")
	rustDir := filepath.Join(dataDir, "rustdesk")
	if err := os.MkdirAll(rustDir, 0700); err != nil {
		log.Fatal(err)
	}
	stg, err := openStore(dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer stg.close()
	if stg.bootstrapRequired() {
		log.Printf("MirvDesk bootstrap token: %s", stg.bootstrapToken)
	}

	hbbs := &child{name: "hbbs", path: env("MIRVDESK_HBBS_BIN", "/usr/local/bin/hbbs"), dir: rustDir}
	hbbr := &child{name: "hbbr", path: env("MIRVDESK_HBBR_BIN", "/usr/local/bin/hbbr"), dir: rustDir}
	hbbrDone, err := hbbr.start()
	if err != nil {
		log.Fatalf("start hbbr: %v", err)
	}
	hbbsDone, err := hbbs.start()
	if err != nil {
		hbbr.stop()
		log.Fatalf("start hbbs: %v", err)
	}

	st := &state{hbbs: hbbs, hbbr: hbbr, store: stg}
	api := &http.Server{
		Addr:              env("MIRVDESK_API_ADDR", "127.0.0.1:21114"),
		Handler:           st.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	apiErr := make(chan error, 1)
	go func() {
		log.Printf("MirvDesk API listening on %s", api.Addr)
		apiErr <- api.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var reason error
	select {
	case <-ctx.Done():
		reason = ctx.Err()
	case err := <-hbbsDone:
		reason = fmt.Errorf("hbbs exited: %w", err)
	case err := <-hbbrDone:
		reason = fmt.Errorf("hbbr exited: %w", err)
	case err := <-apiErr:
		if !errors.Is(err, http.ErrServerClosed) {
			reason = fmt.Errorf("api exited: %w", err)
		}
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = api.Shutdown(shutdown)
	hbbs.stop()
	hbbr.stop()
	if reason != nil && !errors.Is(reason, context.Canceled) {
		log.Printf("shutdown: %v", reason)
	}
}

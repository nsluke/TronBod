// Package server is a tiny HTTP server that exposes the latest stats.json.
// LAN-only by design — no auth.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"
)

type Server struct {
	StatsPath string

	mu     sync.RWMutex
	cached []byte
	when   time.Time
}

// Refresh reads stats.json from disk and caches the bytes. Call this after
// each sync write.
func (s *Server) Refresh() error {
	b, err := os.ReadFile(s.StatsPath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cached = b
	s.when = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/stats.json", s.handleStats)
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	b, when := s.cached, s.when
	s.mu.RUnlock()
	if len(b) == 0 {
		// Fall back to disk on cold start.
		if err := s.Refresh(); err != nil {
			http.Error(w, "stats not yet available", http.StatusServiceUnavailable)
			return
		}
		s.mu.RLock()
		b, when = s.cached, s.when
		s.mu.RUnlock()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Stats-Updated", when.UTC().Format(time.RFC3339))
	_, _ = w.Write(b)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	when := s.when
	s.mu.RUnlock()
	resp := map[string]any{
		"ok":      !when.IsZero(),
		"updated": when.UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	if when.IsZero() {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

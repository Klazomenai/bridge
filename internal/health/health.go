// Package health provides a minimal HTTP health server for Kubernetes probes.
package health

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
)

// Server exposes /healthz (liveness) and /readyz (readiness) endpoints.
type Server struct {
	ready atomic.Bool
	srv   *http.Server
}

// New creates a health server listening on the given port.
// The port may be a bare number ("8080") or prefixed with a colon (":8080").
func New(port string) *Server {
	s := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleLive)
	mux.HandleFunc("GET /readyz", s.handleReady)
	s.srv = &http.Server{
		Addr:    net.JoinHostPort("", strings.TrimPrefix(port, ":")),
		Handler: mux,
	}
	return s
}

// SetReady marks the server as ready. Safe to call from any goroutine.
func (s *Server) SetReady() {
	s.ready.Store(true)
}

// ListenAndServe starts the HTTP server. It blocks until the server is
// shut down or encounters a fatal error.
func (s *Server) ListenAndServe() error {
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// Handler returns the HTTP handler for testing without starting the listener.
func (s *Server) Handler() http.Handler {
	return s.srv.Handler
}

type statusResponse struct {
	Status string `json:"status"`
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statusResponse{Status: "alive"})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(statusResponse{Status: "not ready"})
		return
	}
	_ = json.NewEncoder(w).Encode(statusResponse{Status: "ready"})
}

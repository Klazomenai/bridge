package health_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"klazomenai/bridge/internal/health"
)

func newTestServer(t *testing.T) *health.Server {
	t.Helper()
	// Use port 0 — we won't call ListenAndServe, only test handlers via httptest.
	return health.New("0")
}

type statusResponse struct {
	Status string `json:"status"`
}

func doRequest(t *testing.T, srv *health.Server, path string) (*http.Response, statusResponse) {
	t.Helper()
	// Access the handler via the Server's HTTP handler.
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

	var body statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp, body
}

func TestHealthzAlwaysReturns200(t *testing.T) {
	srv := newTestServer(t)

	resp, body := doRequest(t, srv, "/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if body.Status != "alive" {
		t.Errorf("expected status 'alive', got %q", body.Status)
	}
}

func TestReadyzReturns503BeforeReady(t *testing.T) {
	srv := newTestServer(t)

	resp, body := doRequest(t, srv, "/readyz")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
	if body.Status != "not ready" {
		t.Errorf("expected status 'not ready', got %q", body.Status)
	}
}

func TestReadyzReturns200AfterSetReady(t *testing.T) {
	srv := newTestServer(t)
	srv.SetReady()

	resp, body := doRequest(t, srv, "/readyz")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if body.Status != "ready" {
		t.Errorf("expected status 'ready', got %q", body.Status)
	}
}

func TestSetReadyIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	srv.SetReady()
	srv.SetReady() // should not panic

	resp, _ := doRequest(t, srv, "/readyz")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after double SetReady, got %d", resp.StatusCode)
	}
}

func TestHealthzContentType(t *testing.T) {
	srv := newTestServer(t)

	resp, _ := doRequest(t, srv, "/healthz")
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestReadyzContentTypeNotReady(t *testing.T) {
	srv := newTestServer(t)

	resp, _ := doRequest(t, srv, "/readyz")
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestReadyzContentTypeReady(t *testing.T) {
	srv := newTestServer(t)
	srv.SetReady()

	resp, _ := doRequest(t, srv, "/readyz")
	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

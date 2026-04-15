package lookout_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"klazomenai/bridge/internal/tools/lookout"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// testAllowlist returns a namespace allowlist covering the names used in the
// existing integration tests. Authorization unit tests live in
// authorize_promql_test.go and authorize_logql_test.go.
func testAllowlist() *lookout.NamespaceAllowlist {
	return lookout.NewNamespaceAllowlist([]string{"matrix", "monitoring", "default"})
}

// --- PrometheusQueryTool tests ---

func TestPrometheusQuerySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != `up{namespace="matrix",job="node"}` {
			t.Errorf("unexpected query param: %s", r.URL.Query().Get("query"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	tool := lookout.NewPrometheusQueryTool(srv.URL, testAllowlist(), srv.Client())
	input := mustJSON(t, map[string]string{"query": `up{namespace="matrix",job="node"}`})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "success") {
		t.Errorf("expected success in output, got %q", out)
	}
}

func TestPrometheusQueryWithTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("time") != "2026-03-28T12:00:00Z" {
			t.Errorf("unexpected time param: %s", r.URL.Query().Get("time"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	tool := lookout.NewPrometheusQueryTool(srv.URL, testAllowlist(), srv.Client())
	input := mustJSON(t, map[string]string{"query": `up{namespace="matrix"}`, "time": "2026-03-28T12:00:00Z"})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestPrometheusQueryEmptyQuery(t *testing.T) {
	tool := lookout.NewPrometheusQueryTool("http://localhost:9090", testAllowlist(), lookout.DefaultHTTPClient())
	input := mustJSON(t, map[string]string{"query": ""})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestPrometheusQueryHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	tool := lookout.NewPrometheusQueryTool(srv.URL, testAllowlist(), srv.Client())
	input := mustJSON(t, map[string]string{"query": `up{namespace="matrix"}`})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status code in error, got: %v", err)
	}
}

func TestPrometheusQueryInterface(t *testing.T) {
	tool := lookout.NewPrometheusQueryTool("http://localhost:9090", testAllowlist(), lookout.DefaultHTTPClient())
	if tool.Name() != "prometheus_query" {
		t.Errorf("Name() = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	schema := tool.InputSchema()
	if schema.Properties == nil {
		t.Error("InputSchema().Properties should not be nil")
	}
}

// --- LokiQueryTool tests ---

func TestLokiQuerySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/query_range" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") != `{namespace="matrix",app="bridge"}` {
			t.Errorf("unexpected query: %s", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("limit") != "50" {
			t.Errorf("expected default limit 50, got: %s", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	}))
	defer srv.Close()

	tool := lookout.NewLokiQueryTool(srv.URL, testAllowlist(), srv.Client())
	input := mustJSON(t, map[string]string{"query": `{namespace="matrix",app="bridge"}`})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "success") {
		t.Errorf("expected success in output, got %q", out)
	}
}

func TestLokiQueryLimitClamped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "200" {
			t.Errorf("expected clamped limit 200, got: %s", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	tool := lookout.NewLokiQueryTool(srv.URL, testAllowlist(), srv.Client())
	input := mustJSON(t, map[string]any{"query": `{namespace="matrix",app="bridge"}`, "limit": 500})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestLokiQueryCustomLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("expected limit 100, got: %s", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	tool := lookout.NewLokiQueryTool(srv.URL, testAllowlist(), srv.Client())
	input := mustJSON(t, map[string]any{"query": `{namespace="matrix",app="bridge"}`, "limit": 100})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestLokiQueryWithSince(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start") == "" {
			t.Error("expected start param when since is set")
		}
		if r.URL.Query().Get("end") == "" {
			t.Error("expected end param when since is set")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	tool := lookout.NewLokiQueryTool(srv.URL, testAllowlist(), srv.Client())
	input := mustJSON(t, map[string]any{"query": `{namespace="matrix",app="bridge"}`, "since": "1h"})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestLokiQueryInvalidSince(t *testing.T) {
	tool := lookout.NewLokiQueryTool("http://localhost:3100", testAllowlist(), lookout.DefaultHTTPClient())
	input := mustJSON(t, map[string]any{"query": `{namespace="matrix",app="bridge"}`, "since": "not-a-duration"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for invalid since duration")
	}
}

func TestLokiQueryEmptyQuery(t *testing.T) {
	tool := lookout.NewLokiQueryTool("http://localhost:3100", testAllowlist(), lookout.DefaultHTTPClient())
	input := mustJSON(t, map[string]string{"query": ""})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestLokiQueryHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"parse error"}`))
	}))
	defer srv.Close()

	tool := lookout.NewLokiQueryTool(srv.URL, testAllowlist(), srv.Client())
	input := mustJSON(t, map[string]string{"query": `{namespace="matrix"}`})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected status code in error, got: %v", err)
	}
}

func TestLokiQueryInterface(t *testing.T) {
	tool := lookout.NewLokiQueryTool("http://localhost:3100", testAllowlist(), lookout.DefaultHTTPClient())
	if tool.Name() != "loki_query" {
		t.Errorf("Name() = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	schema := tool.InputSchema()
	if schema.Properties == nil {
		t.Error("InputSchema().Properties should not be nil")
	}
}

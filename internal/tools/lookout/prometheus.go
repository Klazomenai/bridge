// Package lookout provides Lookout's Watch tools: monitoring queries.
package lookout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// HTTPClient is the interface for HTTP requests, allowing test injection.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// DefaultHTTPClient returns an http.Client with a 10s timeout.
func DefaultHTTPClient() HTTPClient {
	return &http.Client{Timeout: 10 * time.Second}
}

// PrometheusQueryTool queries the Prometheus HTTP API.
type PrometheusQueryTool struct {
	baseURL string
	client  HTTPClient
}

// NewPrometheusQueryTool creates a prometheus_query tool.
func NewPrometheusQueryTool(baseURL string, client HTTPClient) *PrometheusQueryTool {
	return &PrometheusQueryTool{baseURL: baseURL, client: client}
}

func (t *PrometheusQueryTool) Name() string { return "prometheus_query" }
func (t *PrometheusQueryTool) Description() string {
	return "Query Prometheus metrics via PromQL."
}

func (t *PrometheusQueryTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "PromQL query expression",
			},
			"time": map[string]any{
				"type":        "string",
				"description": "Evaluation timestamp (RFC3339, omit for current time)",
			},
		},
		Required: []string{"query"},
	}
}

type prometheusInput struct {
	Query string `json:"query"`
	Time  string `json:"time"`
}

func (t *PrometheusQueryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params prometheusInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	u, err := url.Parse(t.baseURL + "/api/v1/query")
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("query", params.Query)
	if params.Time != "" {
		q.Set("time", params.Time)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("prometheus query: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

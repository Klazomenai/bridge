package lookout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

// LokiQueryTool queries the Loki HTTP API.
type LokiQueryTool struct {
	baseURL string
	client  HTTPClient
}

// NewLokiQueryTool creates a loki_query tool.
func NewLokiQueryTool(baseURL string, client HTTPClient) *LokiQueryTool {
	return &LokiQueryTool{baseURL: baseURL, client: client}
}

func (t *LokiQueryTool) Name() string        { return "loki_query" }
func (t *LokiQueryTool) Description() string { return "Query Loki logs via LogQL." }

func (t *LokiQueryTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "LogQL query expression",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of log lines to return (default 50, max 200)",
			},
			"since": map[string]any{
				"type":        "string",
				"description": "Look-back duration (e.g. '1h', '30m'). Omit for Loki default.",
			},
		},
		Required: []string{"query"},
	}
}

type lokiInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	Since string `json:"since"`
}

func (t *LokiQueryTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params lokiInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	u, err := url.Parse(t.baseURL + "/loki/api/v1/query_range")
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("query", params.Query)
	q.Set("limit", strconv.Itoa(limit))

	if params.Since != "" {
		dur, err := time.ParseDuration(params.Since)
		if err != nil {
			return "", fmt.Errorf("invalid since duration %q: %w", params.Since, err)
		}
		now := time.Now()
		q.Set("start", strconv.FormatInt(now.Add(-dur).UnixNano(), 10))
		q.Set("end", strconv.FormatInt(now.UnixNano(), 10))
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("loki query: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("loki returned %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

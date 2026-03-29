package chips

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// GHIssueListTool lists GitHub issues for a repository.
type GHIssueListTool struct {
	execFn    ExecFn
	allowlist RepoAllowlist
	token     string
}

// NewGHIssueListTool creates a gh_issue_list tool.
func NewGHIssueListTool(fn ExecFn, allowlist RepoAllowlist, token string) *GHIssueListTool {
	return &GHIssueListTool{execFn: fn, allowlist: allowlist, token: token}
}

func (t *GHIssueListTool) Name() string        { return "gh_issue_list" }
func (t *GHIssueListTool) Description() string { return "List GitHub issues for a repository." }

func (t *GHIssueListTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"org":   map[string]any{"type": "string", "description": "GitHub organisation or user"},
			"repo":  map[string]any{"type": "string", "description": "Repository name"},
			"state": map[string]any{"type": "string", "description": "Filter by state: open, closed, all (default: open)"},
			"limit": map[string]any{"type": "integer", "description": "Max issues to return (default: 30)"},
		},
		Required: []string{"org", "repo"},
	}
}

type ghIssueListInput struct {
	Org   string `json:"org"`
	Repo  string `json:"repo"`
	State string `json:"state"`
	Limit int    `json:"limit"`
}

func (t *GHIssueListTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params ghIssueListInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if err := t.allowlist.Check(params.Org, params.Repo); err != nil {
		return "", err
	}

	args := []string{"issue", "list", "-R", params.Org + "/" + params.Repo,
		"--json", "number,title,state,labels,author"}

	state := params.State
	if state == "" {
		state = "open"
	}
	args = append(args, "--state", state)

	limit := params.Limit
	if limit <= 0 {
		limit = 30
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit))

	out, err := t.execFn(ctx, "gh", args...)
	if err != nil {
		return "", fmt.Errorf("gh issue list: %w", err)
	}
	return sanitiseOutput(string(out), t.token), nil
}

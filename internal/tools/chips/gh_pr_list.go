package chips

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// GHPRListTool lists GitHub pull requests for a repository.
type GHPRListTool struct {
	execFn    ExecFn
	allowlist RepoAllowlist
	token     string
}

// NewGHPRListTool creates a gh_pr_list tool.
func NewGHPRListTool(fn ExecFn, allowlist RepoAllowlist, token string) *GHPRListTool {
	return &GHPRListTool{execFn: fn, allowlist: allowlist, token: token}
}

func (t *GHPRListTool) Name() string        { return "gh_pr_list" }
func (t *GHPRListTool) Description() string { return "List GitHub pull requests for a repository." }

func (t *GHPRListTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"org":   map[string]any{"type": "string", "description": "GitHub organisation or user"},
			"repo":  map[string]any{"type": "string", "description": "Repository name"},
			"state": map[string]any{"type": "string", "description": "Filter by state: open, closed, merged, all (default: open)"},
			"limit": map[string]any{"type": "integer", "description": "Max PRs to return (default: 30)"},
		},
		Required: []string{"org", "repo"},
	}
}

type ghPRListInput struct {
	Org   string `json:"org"`
	Repo  string `json:"repo"`
	State string `json:"state"`
	Limit int    `json:"limit"`
}

func (t *GHPRListTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params ghPRListInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if err := t.allowlist.Check(params.Org, params.Repo); err != nil {
		return "", err
	}

	args := []string{"pr", "list", "-R", params.Org + "/" + params.Repo,
		"--json", "number,title,state,author,headRefName"}

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
		return "", fmt.Errorf("gh pr list: %w", err)
	}
	return sanitiseOutput(string(out), t.token, t.Name()), nil
}

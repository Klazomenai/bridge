package chips

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// GHPRViewTool views a specific GitHub pull request.
type GHPRViewTool struct {
	execFn    ExecFn
	allowlist RepoAllowlist
	token     string
}

// NewGHPRViewTool creates a gh_pr_view tool.
func NewGHPRViewTool(fn ExecFn, allowlist RepoAllowlist, token string) *GHPRViewTool {
	return &GHPRViewTool{execFn: fn, allowlist: allowlist, token: token}
}

func (t *GHPRViewTool) Name() string        { return "gh_pr_view" }
func (t *GHPRViewTool) Description() string { return "View a GitHub pull request with reviews." }

func (t *GHPRViewTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"org":    map[string]any{"type": "string", "description": "GitHub organisation or user"},
			"repo":   map[string]any{"type": "string", "description": "Repository name"},
			"number": map[string]any{"type": "integer", "description": "Pull request number"},
		},
		Required: []string{"org", "repo", "number"},
	}
}

type ghPRViewInput struct {
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

func (t *GHPRViewTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params ghPRViewInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if err := t.allowlist.Check(params.Org, params.Repo); err != nil {
		return "", err
	}
	if params.Number <= 0 {
		return "", fmt.Errorf("number must be a positive integer")
	}

	args := []string{"pr", "view", fmt.Sprintf("%d", params.Number),
		"-R", params.Org + "/" + params.Repo,
		"--json", "number,title,body,state,reviews,statusCheckRollup"}

	out, err := t.execFn(ctx, "gh", args...)
	if err != nil {
		return "", fmt.Errorf("gh pr view: %w", err)
	}
	return sanitiseOutput(string(out), t.token, t.Name()), nil
}

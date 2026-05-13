package chips

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// GHPRChecksTool checks CI status for a GitHub pull request.
type GHPRChecksTool struct {
	execFn    ExecFn
	allowlist RepoAllowlist
	token     string
}

// NewGHPRChecksTool creates a gh_pr_checks tool.
func NewGHPRChecksTool(fn ExecFn, allowlist RepoAllowlist, token string) *GHPRChecksTool {
	return &GHPRChecksTool{execFn: fn, allowlist: allowlist, token: token}
}

func (t *GHPRChecksTool) Name() string        { return "gh_pr_checks" }
func (t *GHPRChecksTool) Description() string { return "Check CI status for a pull request." }

func (t *GHPRChecksTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"org":    map[string]any{"type": "string", "description": "GitHub organisation or user"},
			"repo":   map[string]any{"type": "string", "description": "Repository name"},
			"number": map[string]any{"type": "integer", "description": "Pull request number"},
		},
		Required: []string{"org", "repo", "number"},
	}
}

type ghPRChecksInput struct {
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

func (t *GHPRChecksTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params ghPRChecksInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if err := t.allowlist.Check(params.Org, params.Repo); err != nil {
		return "", err
	}
	if params.Number <= 0 {
		return "", fmt.Errorf("number must be a positive integer")
	}

	args := []string{"pr", "checks", fmt.Sprintf("%d", params.Number),
		"-R", params.Org + "/" + params.Repo,
		"--json", "name,status,conclusion"}

	out, err := t.execFn(ctx, "gh", args...)
	if err != nil {
		return "", fmt.Errorf("gh pr checks: %w", err)
	}
	return sanitiseOutput(string(out), t.token, t.Name()), nil
}

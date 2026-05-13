package chips

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// GHIssueViewTool views a specific GitHub issue.
type GHIssueViewTool struct {
	execFn    ExecFn
	allowlist RepoAllowlist
	token     string
}

// NewGHIssueViewTool creates a gh_issue_view tool.
func NewGHIssueViewTool(fn ExecFn, allowlist RepoAllowlist, token string) *GHIssueViewTool {
	return &GHIssueViewTool{execFn: fn, allowlist: allowlist, token: token}
}

func (t *GHIssueViewTool) Name() string        { return "gh_issue_view" }
func (t *GHIssueViewTool) Description() string { return "View a GitHub issue with comments." }

func (t *GHIssueViewTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"org":    map[string]any{"type": "string", "description": "GitHub organisation or user"},
			"repo":   map[string]any{"type": "string", "description": "Repository name"},
			"number": map[string]any{"type": "integer", "description": "Issue number"},
		},
		Required: []string{"org", "repo", "number"},
	}
}

type ghIssueViewInput struct {
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

func (t *GHIssueViewTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params ghIssueViewInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if err := t.allowlist.Check(params.Org, params.Repo); err != nil {
		return "", err
	}
	if params.Number <= 0 {
		return "", fmt.Errorf("number must be a positive integer")
	}

	args := []string{"issue", "view", fmt.Sprintf("%d", params.Number),
		"-R", params.Org + "/" + params.Repo,
		"--json", "number,title,body,state,comments,labels"}

	out, err := t.execFn(ctx, "gh", args...)
	if err != nil {
		return "", fmt.Errorf("gh issue view: %w", err)
	}
	return sanitiseOutput(string(out), t.token, t.Name()), nil
}

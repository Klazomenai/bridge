package chips

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// GHIssueCreateTool creates a GitHub issue. It implements MutationAware
// so the orchestrator marks the invocation as mutation:true in the audit
// log and applies the operator-intent rule from _universal.md.
type GHIssueCreateTool struct {
	execFn    ExecFn
	allowlist RepoAllowlist
	token     string
}

// NewGHIssueCreateTool creates a gh_issue_create tool.
func NewGHIssueCreateTool(fn ExecFn, allowlist RepoAllowlist, token string) *GHIssueCreateTool {
	return &GHIssueCreateTool{execFn: fn, allowlist: allowlist, token: token}
}

func (t *GHIssueCreateTool) Name() string        { return "gh_issue_create" }
func (t *GHIssueCreateTool) Description() string { return "Create a GitHub issue." }

// Mutation satisfies MutationAware. Creating an issue is a state-mutating
// operation; the orchestrator marks audit records with mutation:true and
// applies the operator-intent rule.
func (t *GHIssueCreateTool) Mutation() bool { return true }

func (t *GHIssueCreateTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"org":       map[string]any{"type": "string", "description": "GitHub organisation or user"},
			"repo":      map[string]any{"type": "string", "description": "Repository name"},
			"title":     map[string]any{"type": "string", "description": "Issue title"},
			"body":      map[string]any{"type": "string", "description": "Issue body (Markdown)"},
			"labels":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Labels to apply (optional)"},
			"assignees": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "GitHub usernames to assign (optional)"},
		},
		Required: []string{"org", "repo", "title", "body"},
	}
}

type ghIssueCreateInput struct {
	Org       string   `json:"org"`
	Repo      string   `json:"repo"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels"`
	Assignees []string `json:"assignees"`
}

func (t *GHIssueCreateTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params ghIssueCreateInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if err := t.allowlist.Check(params.Org, params.Repo); err != nil {
		return "", err
	}
	if params.Title == "" {
		return "", fmt.Errorf("title must not be empty")
	}

	args := []string{
		"issue", "create",
		"-R", params.Org + "/" + params.Repo,
		"--title", params.Title,
		"--body", params.Body,
	}
	for _, l := range params.Labels {
		args = append(args, "--label", l)
	}
	for _, a := range params.Assignees {
		args = append(args, "--assignee", a)
	}

	out, err := t.execFn(ctx, "gh", args...)
	if err != nil {
		return "", fmt.Errorf("gh issue create: %w", err)
	}
	return sanitiseOutput(string(out), t.token, t.Name()), nil
}

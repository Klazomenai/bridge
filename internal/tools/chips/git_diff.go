package chips

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// GitDiffTool shows the diff between two git refs.
type GitDiffTool struct {
	execFn ExecFn
	token  string
}

// NewGitDiffTool creates a git_diff tool.
func NewGitDiffTool(fn ExecFn, token string) *GitDiffTool {
	return &GitDiffTool{execFn: fn, token: token}
}

func (t *GitDiffTool) Name() string        { return "git_diff" }
func (t *GitDiffTool) Description() string { return "View git diff between two refs." }

func (t *GitDiffTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"ref1": map[string]any{"type": "string", "description": "Base ref (e.g. main, commit SHA)"},
			"ref2": map[string]any{"type": "string", "description": "Head ref (default: HEAD)"},
		},
		Required: []string{"ref1"},
	}
}

type gitDiffInput struct {
	Ref1 string `json:"ref1"`
	Ref2 string `json:"ref2"`
}

func (t *GitDiffTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params gitDiffInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	ref1 := strings.TrimSpace(params.Ref1)
	if ref1 == "" {
		return "", fmt.Errorf("ref1 is required")
	}
	if strings.HasPrefix(ref1, "-") {
		return "", fmt.Errorf("invalid ref1: must not start with '-'")
	}

	ref2 := strings.TrimSpace(params.Ref2)
	if ref2 == "" {
		ref2 = "HEAD"
	}
	if strings.HasPrefix(ref2, "-") {
		return "", fmt.Errorf("invalid ref2: must not start with '-'")
	}

	args := []string{"diff", ref1 + ".." + ref2}

	out, err := t.execFn(ctx, "git", args...)
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return sanitiseOutput(string(out), t.token, t.Name()), nil
}

package chips

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

const (
	defaultLogCount = 20
	maxLogCount     = 100
)

// GitLogTool shows recent git commits.
type GitLogTool struct {
	execFn ExecFn
	token  string
}

// NewGitLogTool creates a git_log tool.
func NewGitLogTool(fn ExecFn, token string) *GitLogTool {
	return &GitLogTool{execFn: fn, token: token}
}

func (t *GitLogTool) Name() string        { return "git_log" }
func (t *GitLogTool) Description() string { return "View recent git commits." }

func (t *GitLogTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"count": map[string]any{"type": "integer", "description": "Number of commits to show (default: 20, max: 100)"},
			"ref":   map[string]any{"type": "string", "description": "Branch or ref to show (default: HEAD)"},
		},
	}
}

type gitLogInput struct {
	Count int    `json:"count"`
	Ref   string `json:"ref"`
}

func (t *GitLogTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params gitLogInput
	if len(input) > 0 {
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
	}

	count := params.Count
	if count <= 0 {
		count = defaultLogCount
	}
	if count > maxLogCount {
		count = maxLogCount
	}

	ref := strings.TrimSpace(params.Ref)
	if ref != "" && strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("invalid ref: must not start with '-'")
	}

	args := []string{"log", "--oneline", fmt.Sprintf("-n%d", count)}
	if ref != "" {
		args = append(args, "--", ref)
	}

	out, err := t.execFn(ctx, "git", args...)
	if err != nil {
		return "", fmt.Errorf("git log: %w", err)
	}
	return sanitiseOutput(string(out), t.token, t.Name()), nil
}

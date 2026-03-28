package tools

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// stubTool is a placeholder tool registered when the real implementation is
// not available (e.g. missing binary, env vars not set). It passes ValidateTools
// but returns an error with the stub's description when Claude tries to use it.
type stubTool struct {
	name string
	desc string
}

// NewStubTool creates a tool that exists in the registry but returns
// "not available" with the given description when executed.
func NewStubTool(name, description string) ToolDefinition {
	return &stubTool{name: name, desc: description}
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return s.desc }
func (s *stubTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
}
func (s *stubTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "", fmt.Errorf("tool %q is not available — %s", s.name, s.desc)
}

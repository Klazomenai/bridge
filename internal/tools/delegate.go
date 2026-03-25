package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

const (
	// DelegateToolName is the tool name registered for crew-to-crew delegation.
	DelegateToolName = "delegate_to_crew"
	// DelegateSentinel is the prefix used to detect delegation results in the
	// orchestrator tool loop. The format is "DELEGATE:<crewID>:<context>".
	DelegateSentinel = "DELEGATE:"
)

// delegateInput is the JSON schema input for delegate_to_crew.
type delegateInput struct {
	Crew    string `json:"crew"`
	Context string `json:"context"`
}

// DelegateTool enables crew-to-crew delegation within the same message flow.
// It returns a sentinel string that the orchestrator intercepts to trigger a
// new Handle() call with the target crew. The sentinel never reaches Claude
// as a tool_result.
type DelegateTool struct{}

func (d *DelegateTool) Name() string        { return DelegateToolName }
func (d *DelegateTool) Description() string { return "Delegate the conversation to another crew member" }
func (d *DelegateTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"crew": map[string]any{
				"type":        "string",
				"description": "Crew member ID to delegate to (e.g. crest, maren)",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Brief context for the delegate explaining what they should do",
			},
		},
		Required: []string{"crew", "context"},
	}
}

func (d *DelegateTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var params delegateInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	params.Crew = strings.TrimSpace(strings.ToLower(params.Crew))
	params.Context = strings.TrimSpace(params.Context)

	if params.Crew == "" {
		return "", fmt.Errorf("crew is required")
	}
	if params.Context == "" {
		return "", fmt.Errorf("context is required")
	}

	return fmt.Sprintf("%s%s:%s", DelegateSentinel, params.Crew, params.Context), nil
}

// ParseDelegation extracts the crew ID and context from a delegation sentinel
// string. Returns ("", "", false) if the string is not a delegation sentinel.
func ParseDelegation(result string) (crewID, delegateContext string, ok bool) {
	if !strings.HasPrefix(result, DelegateSentinel) {
		return "", "", false
	}
	rest := result[len(DelegateSentinel):]
	idx := strings.Index(rest, ":")
	if idx == -1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

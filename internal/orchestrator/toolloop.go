package orchestrator

import (
	"context"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"klazomenai/bridge/internal/crew"
)

const (
	// maxToolIterations caps the number of tool-use round-trips per message.
	maxToolIterations = 5
	// maxToolOutputLen caps individual tool output to prevent context bloat.
	maxToolOutputLen = 4096
)

// loopResult holds the output of a tool-use loop.
type loopResult struct {
	// text is the final assistant text after all tool-use rounds complete.
	text string
	// turns collects all intermediate messages (assistant tool_use + user tool_result)
	// for storage in the context buffer.
	turns []anthropic.MessageParam
}

// runToolLoop calls the Claude API and, if the response contains tool_use blocks,
// executes the requested tools and continues until Claude produces a text response
// or the iteration limit is reached.
//
// For now (pre-#32), this is a pass-through: no tools are registered, so Claude
// never returns tool_use. The loop structure is here for #32 to fill in.
func (o *Orchestrator) runToolLoop(ctx context.Context, c crew.Crew, messages []anthropic.MessageParam) (*loopResult, error) {
	resp, err := o.client.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.Model()),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: c.SystemPrompt()},
		},
		Messages: messages,
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic api: %w", err)
	}

	text := extractText(resp)
	if text == "" {
		return nil, fmt.Errorf("anthropic returned no text content (got %d block(s), types may be tool-use only)", len(resp.Content))
	}

	return &loopResult{text: text}, nil
}

// extractText concatenates all text blocks from a Claude response.
func extractText(resp *anthropic.Message) string {
	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

// buildResponse constructs the orchestrator Response from a crew member and text.
func buildResponse(c crew.Crew, text string) *Response {
	return &Response{
		Text:       text,
		CrewID:     c.ID(),
		CrewMember: c.Name(),
		Verbosity:  c.Verbosity(),
	}
}

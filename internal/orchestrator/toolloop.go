package orchestrator

import (
	"context"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"klazomenai/bridge/internal/crew"
)

// loopResult holds the output of a tool-use loop.
type loopResult struct {
	// text is the final assistant text after all tool-use rounds complete.
	text string
	// turns collects all intermediate messages (assistant tool_use + user tool_result)
	// for storage in the context buffer.
	turns []anthropic.MessageParam
}

// runToolLoop makes a single Claude API call and returns the assistant text.
//
// This is a stub: no tool-use handling or iterative looping is implemented yet.
// Full tool_use support will be added in #32.
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

// frameMessage caps and prefixes the user input to limit prompt injection surface.
func frameMessage(userText string) string {
	// Fast path: byte length <= cap means rune count must also be <= cap (UTF-8 invariant).
	// Only pay for rune conversion on the uncommon long-message path.
	if len(userText) > maxUserMessageLen {
		if runes := []rune(userText); len(runes) > maxUserMessageLen {
			userText = string(runes[:maxUserMessageLen])
		}
	}
	return captainPrefix + userText
}

package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"klazomenai/bridge/internal/crew"
	"klazomenai/bridge/internal/tools"
)

const (
	// maxToolIterations caps the number of tool-use round-trips per message.
	maxToolIterations = 5
)

// loopResult holds the output of a tool-use loop.
type loopResult struct {
	// text is the final assistant text after all tool-use rounds complete.
	text string
	// turns collects all intermediate messages (assistant tool_use + user tool_result)
	// for storage in the context buffer.
	turns []anthropic.MessageParam
}

// runToolLoop calls the Claude API with the crew's tools. If Claude responds
// with tool_use blocks, the bridge executes the requested tools and feeds
// results back. This continues until Claude produces a text response or the
// iteration limit is reached.
func (o *Orchestrator) runToolLoop(ctx context.Context, c crew.Crew, roomID string, messages []anthropic.MessageParam) (*loopResult, error) {
	crewTools := o.tools.ForCrew(c.Tools())

	var turns []anthropic.MessageParam

	for i := range maxToolIterations {
		resp, err := o.client.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(c.Model()),
			MaxTokens: maxTokens,
			System: []anthropic.TextBlockParam{
				{Text: c.SystemPrompt()},
			},
			Messages: messages,
			Tools:    crewTools,
		})
		if err != nil {
			return nil, fmt.Errorf("anthropic api (iteration %d): %w", i, err)
		}

		// If Claude is done talking, extract text and return.
		if resp.StopReason != anthropic.StopReasonToolUse {
			text := extractText(resp)
			if text == "" {
				return nil, fmt.Errorf("anthropic returned no text content (stop_reason=%s, %d block(s))",
					resp.StopReason, len(resp.Content))
			}
			return &loopResult{text: text, turns: turns}, nil
		}

		// Claude wants to use tools — execute each one and collect results.
		allowedTools := c.Tools()
		toolResults := o.executeToolCalls(ctx, resp.Content, allowedTools, c.ID(), roomID)

		// Build the assistant message preserving text and tool_use blocks.
		assistantBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(resp.Content))
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				assistantBlocks = append(assistantBlocks, anthropic.NewTextBlock(block.Text))
			case "tool_use":
				assistantBlocks = append(assistantBlocks, anthropic.NewToolUseBlock(block.ID, block.Input, block.Name))
			default:
				slog.Debug("orchestrator: skipping unknown content block type",
					"type", block.Type)
			}
		}
		assistantMsg := anthropic.NewAssistantMessage(assistantBlocks...)
		userMsg := anthropic.NewUserMessage(toolResults...)

		turns = append(turns, assistantMsg, userMsg)
		messages = append(messages, assistantMsg, userMsg)

		slog.Info("orchestrator: tool-use round complete",
			"iteration", i+1, "tool_results", len(toolResults),
			"crew", c.Name())
	}

	return nil, fmt.Errorf("tool-use loop exceeded %d iterations", maxToolIterations)
}

// executeToolCalls runs each tool_use block and returns tool_result blocks.
// allowedTools is the crew member's declared tool allowlist for defense in depth.
func (o *Orchestrator) executeToolCalls(ctx context.Context, content []anthropic.ContentBlockUnion, allowedTools []string, crewID, roomID string) []anthropic.ContentBlockParamUnion {
	allowed := make(map[string]bool, len(allowedTools))
	for _, t := range allowedTools {
		allowed[t] = true
	}

	var results []anthropic.ContentBlockParamUnion

	for _, block := range content {
		if block.Type != "tool_use" {
			continue
		}

		if !allowed[block.Name] {
			slog.Warn("orchestrator: tool not in crew allowlist",
				"tool", block.Name, "allowed", allowedTools)
			results = append(results, anthropic.NewToolResultBlock(
				block.ID, fmt.Sprintf("tool %q not allowed for this crew member", block.Name), true))
			continue
		}

		tool := o.tools.Get(block.Name)
		if tool == nil {
			slog.Warn("orchestrator: unknown tool requested",
				"tool", block.Name,
				"crew", crewID,
				"room", roomID,
			)
			results = append(results, anthropic.NewToolResultBlock(
				block.ID, fmt.Sprintf("tool error: unknown tool: %q", block.Name), true))
			continue
		}

		meta := tools.SandboxMeta{CrewID: crewID, RoomID: roomID, ToolName: block.Name}
		result, isError := tools.ExecuteWithSandbox(ctx, tool, block.Input, o.sandboxCfg, meta)
		results = append(results, anthropic.NewToolResultBlock(block.ID, result, isError))
	}

	return results
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

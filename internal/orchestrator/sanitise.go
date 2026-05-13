package orchestrator

import (
	"log/slog"

	"klazomenai/bridge/internal/tools/redact"
)

// sanitiseToolResultContent applies the shared redact.Sanitise pass
// to a tool_result content string before it joins the message history
// sent to Claude. This is the orchestrator-level safety floor
// described in #129: per-tool sanitisers (chips, maren, crest) stay
// as the first line of defence; this layer guarantees that every
// tool_result reaching the model has passed through the shared
// pattern set, regardless of whether the per-tool layer remembered
// to sanitise.
//
// Emits with layer="orchestrator_floor" so operators can grep floor-
// level catches distinct from per-tool emissions: a floor emission
// means the per-tool layer either missed the shape or never ran (a
// new tool author or a third-party tool integration that bypassed
// the chips-style helper). In steady state with per-tool sanitisers
// doing their job, this layer emits nothing — content reaching it is
// already redacted, so FindAllStringIndex returns zero matches and
// no slog line fires.
//
// Inherits the fail-closed, byte-cap, and clone-on-truncate contracts
// from redact.SanitiseWith.
func sanitiseToolResultContent(toolName, content string) string {
	// The tool name on the allowlist-refusal and unknown-tool paths
	// is model-supplied (block.Name from Claude's tool_use response)
	// and is NOT validated against any registry — Claude can return
	// any string as a tool name, including a token-shaped value. If
	// the raw name were used as the slog `tool` attribute, the floor
	// would redact the leak from the tool_result content but
	// reintroduce it in its own attribution log. Apply Sanitise to
	// the name first; for legitimate registered tool names
	// (delegate_to_crew, gh_issue_view, ...) this is a no-op because
	// no default pattern matches a typical tool name. The outer
	// Sanitise on the content does the actual content redaction
	// AND emits the slog line with the now-safe name attribute.
	safeToolAttr := redact.Sanitise(toolName)
	return redact.Sanitise(content,
		slog.String("layer", "orchestrator_floor"),
		slog.String("tool", safeToolAttr),
		slog.String("field", "tool_result"),
	)
}

// SanitiseToolResultContentForTest is an exported alias for testing.
// Test code in package orchestrator_test uses it to drive the
// floor's idempotence, length-ceiling, and fail-closed behaviour
// directly without setting up the full orchestrator pipeline. Do
// NOT call from production code — use the orchestrator's tool-use
// loop instead.
var SanitiseToolResultContentForTest = sanitiseToolResultContent

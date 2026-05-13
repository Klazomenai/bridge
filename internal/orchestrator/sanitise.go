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
	return redact.Sanitise(content,
		slog.String("layer", "orchestrator_floor"),
		slog.String("tool", toolName),
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

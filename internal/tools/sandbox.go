package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"klazomenai/bridge/internal/tools/redact"
)

const (
	// DefaultMaxOutputLen caps individual tool output by rune count to prevent
	// context bloat while remaining UTF-8 safe.
	DefaultMaxOutputLen = 4096
	// DefaultTimeout is the per-tool execution timeout.
	DefaultTimeout = 30 * time.Second
)

// SandboxConfig holds the limits applied to every tool execution.
type SandboxConfig struct {
	Timeout      time.Duration
	MaxOutputLen int
}

// SandboxMeta carries per-invocation context for structured audit logging.
//
// Fields:
//   - CrewID, RoomID, ToolName — identifying context for the invocation
//   - Mutation — whether the tool mutates external state (set by callers
//     via tools.IsMutation(tool))
//   - Logger — destination for the structured audit + execution events
//     emitted by ExecuteWithSandbox; nil falls back to slog.Default()
//   - Secrets — values to redact from the audit log's argv field via
//     redact.Redact (e.g. GITHUB_TOKEN); empty entries are skipped
type SandboxMeta struct {
	CrewID   string
	RoomID   string
	ToolName string
	Mutation bool
	Logger   *slog.Logger
	Secrets  []string
}

// logger returns the SandboxMeta's logger, falling back to slog.Default()
// when nil. Callers within this package use this helper instead of the
// package-level slog.* functions so per-invocation logger injection works
// for tests and future per-room routing.
func (m SandboxMeta) logger() *slog.Logger {
	if m.Logger != nil {
		return m.Logger
	}
	return slog.Default()
}

// DefaultSandboxConfig returns a SandboxConfig with production defaults.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		Timeout:      DefaultTimeout,
		MaxOutputLen: DefaultMaxOutputLen,
	}
}

// truncateForLog returns s capped at maxRunes runes, with a "\n[truncated]"
// marker appended when truncation occurred. Fast-path: byte length <=
// maxRunes implies rune count <= maxRunes (UTF-8 invariant), so the
// rune-conversion is only paid on the uncommon long-input path.
//
// Used by ExecuteWithSandbox at three sites: error messages, tool
// output, and the audit log's argv_redacted field. Centralised so the
// rune-safety invariant cannot drift across sites.
func truncateForLog(s string, maxRunes int) string {
	if len(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "\n[truncated]"
}

// ExecuteWithSandbox runs a tool within a cooperative timeout, recovers panics,
// caps output, and emits structured audit logs. Returns the result string and
// whether the result represents an error.
//
// The timeout is cooperative: it relies on the tool honouring ctx.Done(). A tool
// that blocks without checking context will stall the caller. Most built-in tools
// are implemented to respect context cancellation. If non-cooperative tools are
// introduced, consider wrapping execution in a goroutine with select on ctx.Done()
// — noting the goroutine-leak tradeoff.
func ExecuteWithSandbox(ctx context.Context, tool ToolDefinition, input json.RawMessage,
	cfg SandboxConfig, meta SandboxMeta) (result string, isError bool) {

	// Clamp invalid MaxOutputLen to the default to prevent slice panics.
	if cfg.MaxOutputLen <= 0 {
		cfg.MaxOutputLen = DefaultMaxOutputLen
	}

	logger := meta.logger()

	// Audit record — emitted before execution so an in-flight panic or
	// timeout still leaves a trail of the attempted invocation. The
	// argv field is redacted using meta.Secrets to keep tokens out of
	// the log destination (stdout, journald, downstream collectors)
	// AND truncated to MaxOutputLen so a tool with a giant input
	// payload cannot blow up downstream collectors or smuggle large
	// non-secret-but-sensitive content past the redaction layer.
	logger.Info("audit: tool invoked",
		"tool", meta.ToolName,
		"crew", meta.CrewID,
		"room", meta.RoomID,
		"mutation", meta.Mutation,
		"argv_redacted", truncateForLog(redact.Redact(string(input), meta.Secrets...), cfg.MaxOutputLen),
	)

	start := time.Now()

	toolCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Panic recovery — a misbehaving tool must not crash the pod.
	var output string
	var execErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				execErr = fmt.Errorf("tool panicked: %v", r)
			}
		}()
		output, execErr = tool.Execute(toolCtx, input)
	}()

	elapsed := time.Since(start)

	if execErr != nil {
		// Truncate error output rune-safely so tools cannot bypass
		// MaxOutputLen via oversized error messages.
		errMsg := truncateForLog(execErr.Error(), cfg.MaxOutputLen)

		logger.Warn("sandbox: tool execution failed",
			"tool", meta.ToolName, "crew", meta.CrewID,
			"room", meta.RoomID, "duration_ms", elapsed.Milliseconds(),
			"err", execErr)
		return fmt.Sprintf("tool error: %s", errMsg), true
	}

	output = truncateForLog(output, cfg.MaxOutputLen)

	logger.Info("sandbox: tool executed",
		"tool", meta.ToolName, "crew", meta.CrewID,
		"room", meta.RoomID, "duration_ms", elapsed.Milliseconds(),
		"output_len_bytes", len(output))

	return output, false
}

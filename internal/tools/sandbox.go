package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
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
type SandboxMeta struct {
	CrewID   string
	RoomID   string
	ToolName string
}

// DefaultSandboxConfig returns a SandboxConfig with production defaults.
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		Timeout:      DefaultTimeout,
		MaxOutputLen: DefaultMaxOutputLen,
	}
}

// ExecuteWithSandbox runs a tool within a timeout, recovers panics, caps output,
// and emits structured audit logs. Returns the result string and whether the
// result represents an error.
func ExecuteWithSandbox(ctx context.Context, tool ToolDefinition, input json.RawMessage,
	cfg SandboxConfig, meta SandboxMeta) (result string, isError bool) {

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
		slog.Warn("sandbox: tool execution failed",
			"tool", meta.ToolName, "crew", meta.CrewID,
			"room", meta.RoomID, "duration_ms", elapsed.Milliseconds(),
			"err", execErr)
		return fmt.Sprintf("tool error: %s", execErr.Error()), true
	}

	// Fast path: byte length <= cap means rune count must also be <= cap
	// (UTF-8 invariant). Only pay for rune conversion on the uncommon
	// long-output path.
	if len(output) > cfg.MaxOutputLen {
		if runes := []rune(output); len(runes) > cfg.MaxOutputLen {
			output = string(runes[:cfg.MaxOutputLen]) + "\n[truncated]"
		}
	}

	slog.Info("sandbox: tool executed",
		"tool", meta.ToolName, "crew", meta.CrewID,
		"room", meta.RoomID, "duration_ms", elapsed.Milliseconds(),
		"output_len", len(output))

	return output, false
}

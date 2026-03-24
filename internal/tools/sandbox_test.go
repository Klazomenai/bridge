package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"klazomenai/bridge/internal/tools"
)

// testTool is a configurable tool for sandbox tests.
type testTool struct {
	name    string
	execFn  func(ctx context.Context, input json.RawMessage) (string, error)
}

func (t *testTool) Name() string        { return t.name }
func (t *testTool) Description() string { return "test tool" }
func (t *testTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
}
func (t *testTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return t.execFn(ctx, input)
}

func testMeta() tools.SandboxMeta {
	return tools.SandboxMeta{CrewID: "maren", RoomID: "!test:localhost", ToolName: "test_tool"}
}

func TestExecuteWithSandbox(t *testing.T) {
	tests := []struct {
		name       string
		tool       tools.ToolDefinition
		cfg        tools.SandboxConfig
		wantErr    bool
		wantSubstr string
	}{
		{
			name: "happy path",
			tool: &testTool{name: "ok", execFn: func(_ context.Context, _ json.RawMessage) (string, error) {
				return "all good", nil
			}},
			cfg:        tools.DefaultSandboxConfig(),
			wantErr:    false,
			wantSubstr: "all good",
		},
		{
			name: "tool error",
			tool: &testTool{name: "fail", execFn: func(_ context.Context, _ json.RawMessage) (string, error) {
				return "", fmt.Errorf("broken")
			}},
			cfg:        tools.DefaultSandboxConfig(),
			wantErr:    true,
			wantSubstr: "tool error: broken",
		},
		{
			name: "panic recovery",
			tool: &testTool{name: "panic", execFn: func(_ context.Context, _ json.RawMessage) (string, error) {
				panic("kaboom")
			}},
			cfg:        tools.DefaultSandboxConfig(),
			wantErr:    true,
			wantSubstr: "tool panicked: kaboom",
		},
		{
			name: "timeout",
			tool: &testTool{name: "slow", execFn: func(ctx context.Context, _ json.RawMessage) (string, error) {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(5 * time.Second):
					return "too late", nil
				}
			}},
			cfg:        tools.SandboxConfig{Timeout: 50 * time.Millisecond, MaxOutputLen: 4096},
			wantErr:    true,
			wantSubstr: "context deadline exceeded",
		},
		{
			name: "output cap",
			tool: &testTool{name: "large", execFn: func(_ context.Context, _ json.RawMessage) (string, error) {
				return strings.Repeat("x", 10000), nil
			}},
			cfg:        tools.SandboxConfig{Timeout: 30 * time.Second, MaxOutputLen: 100},
			wantErr:    false,
			wantSubstr: "[truncated]",
		},
		{
			name: "utf8 rune boundary",
			tool: &testTool{name: "utf8", execFn: func(_ context.Context, _ json.RawMessage) (string, error) {
				// Each rune is 3 bytes. 50 runes = 150 bytes. Cap at 10 runes.
				return strings.Repeat("日", 50), nil
			}},
			cfg:     tools.SandboxConfig{Timeout: 30 * time.Second, MaxOutputLen: 10},
			wantErr: false,
			wantSubstr: strings.Repeat("日", 10) + "\n[truncated]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, isError := tools.ExecuteWithSandbox(
				context.Background(), tt.tool, nil, tt.cfg, testMeta())

			if isError != tt.wantErr {
				t.Errorf("isError = %v, want %v", isError, tt.wantErr)
			}
			if !strings.Contains(result, tt.wantSubstr) {
				t.Errorf("result = %q, want substring %q", result, tt.wantSubstr)
			}
		})
	}
}

func TestExecuteWithSandbox_OutputNotTruncatedAtLimit(t *testing.T) {
	// Output exactly at the limit should NOT be truncated.
	tool := &testTool{name: "exact", execFn: func(_ context.Context, _ json.RawMessage) (string, error) {
		return strings.Repeat("a", 100), nil
	}}
	cfg := tools.SandboxConfig{Timeout: 30 * time.Second, MaxOutputLen: 100}

	result, isError := tools.ExecuteWithSandbox(context.Background(), tool, nil, cfg, testMeta())
	if isError {
		t.Fatal("unexpected error")
	}
	if strings.Contains(result, "[truncated]") {
		t.Error("output at exact limit should not be truncated")
	}
	if len(result) != 100 {
		t.Errorf("expected length 100, got %d", len(result))
	}
}

func TestExecuteWithSandbox_InvalidMaxOutputLenClamped(t *testing.T) {
	// A zero or negative MaxOutputLen must not panic — it should clamp to default.
	tool := &testTool{name: "big", execFn: func(_ context.Context, _ json.RawMessage) (string, error) {
		return strings.Repeat("x", 5000), nil
	}}
	cfg := tools.SandboxConfig{Timeout: 30 * time.Second, MaxOutputLen: 0}

	result, isError := tools.ExecuteWithSandbox(context.Background(), tool, nil, cfg, testMeta())
	if isError {
		t.Fatal("unexpected error")
	}
	if !strings.Contains(result, "[truncated]") {
		t.Error("expected truncation at default cap when MaxOutputLen is 0")
	}
	// Clamped to DefaultMaxOutputLen (4096) + len("\n[truncated]") = 4108
	if len(result) != tools.DefaultMaxOutputLen+len("\n[truncated]") {
		t.Errorf("expected length %d, got %d", tools.DefaultMaxOutputLen+len("\n[truncated]"), len(result))
	}
}

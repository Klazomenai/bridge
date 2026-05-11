package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	ctxbuf "klazomenai/bridge/internal/context"
	"klazomenai/bridge/internal/crew"
	"klazomenai/bridge/internal/orchestrator"
	"klazomenai/bridge/internal/testutil"
	"klazomenai/bridge/internal/tools"
	chipstools "klazomenai/bridge/internal/tools/chips"
)

const testCrewYAML = `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [delegate_to_crew]
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
  crest:
    name: "Crest"
    role: "Signalman"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [delegate_to_crew, echo_tool]
    voice:
      model: "en_US-lessac-high.onnx"
      announces_as: "Crest:"
    system_prompt: "You are Crest. Respond in {verbosity}"
`

// mockClaudeClient supports multi-call sequences for tool-use testing.
// Use callErrors (per-call) for recovery tests; use err (global) for
// single-error-on-every-call tests.
type mockClaudeClient struct {
	responses  []*anthropic.Message // returned in order; last one repeats if exhausted
	err        error                // returned on every call (backward compat)
	callErrors []error              // per-call errors; takes priority when non-empty
	calls      []anthropic.MessageNewParams
	callIndex  int
}

func (m *mockClaudeClient) New(_ context.Context, body anthropic.MessageNewParams, _ ...option.RequestOption) (*anthropic.Message, error) {
	m.calls = append(m.calls, body)
	idx := m.callIndex

	// Per-call errors take priority over the global err field.
	if len(m.callErrors) > 0 {
		errIdx := idx
		if errIdx >= len(m.callErrors) {
			errIdx = len(m.callErrors) - 1
		}
		if m.callErrors[errIdx] != nil {
			m.callIndex++
			return nil, m.callErrors[errIdx]
		}
	} else if m.err != nil {
		return nil, m.err
	}

	respIdx := idx
	if respIdx >= len(m.responses) {
		respIdx = len(m.responses) - 1
	}
	m.callIndex++
	return m.responses[respIdx], nil
}

func textResponse(text string) *anthropic.Message {
	return &anthropic.Message{
		StopReason: anthropic.StopReasonEndTurn,
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: text},
		},
	}
}

func toolUseResponse(toolID, toolName string, input json.RawMessage) *anthropic.Message {
	return &anthropic.Message{
		StopReason: anthropic.StopReasonToolUse,
		Content: []anthropic.ContentBlockUnion{
			{Type: "tool_use", ID: toolID, Name: toolName, Input: input},
		},
	}
}

func toolUseWithTextResponse(text, toolID, toolName string, input json.RawMessage) *anthropic.Message {
	return &anthropic.Message{
		StopReason: anthropic.StopReasonToolUse,
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: text},
			{Type: "tool_use", ID: toolID, Name: toolName, Input: input},
		},
	}
}

func multiToolUseResponse(calls ...struct {
	ID    string
	Name  string
	Input json.RawMessage
}) *anthropic.Message {
	blocks := make([]anthropic.ContentBlockUnion, len(calls))
	for i, c := range calls {
		blocks[i] = anthropic.ContentBlockUnion{Type: "tool_use", ID: c.ID, Name: c.Name, Input: c.Input}
	}
	return &anthropic.Message{
		StopReason: anthropic.StopReasonToolUse,
		Content:    blocks,
	}
}

// echoTool is a test tool that returns its input as output.
type echoTool struct{}

func (e *echoTool) Name() string        { return "echo_tool" }
func (e *echoTool) Description() string { return "echoes input" }
func (e *echoTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{"msg": map[string]string{"type": "string"}},
	}
}
func (e *echoTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}
	return "echo: " + params.Msg, nil
}

// failTool always returns an error.
type failTool struct{}

func (f *failTool) Name() string        { return "fail_tool" }
func (f *failTool) Description() string { return "always fails" }
func (f *failTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
}
func (f *failTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "", errors.New("tool broke")
}

// largeTool returns output exceeding maxToolOutputLen.
type largeTool struct{}

func (l *largeTool) Name() string        { return "large_tool" }
func (l *largeTool) Description() string { return "returns large output" }
func (l *largeTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
}
func (l *largeTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return strings.Repeat("x", 10000), nil
}

func newTestRegistry(t *testing.T) *crew.Registry {
	t.Helper()
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(testCrewYAML), 0o600); err != nil {
		t.Fatalf("write crew yaml: %v", err)
	}
	r, err := crew.Load(f)
	if err != nil {
		t.Fatalf("load crew: %v", err)
	}
	return r
}

func newToolRegistry(tt ...tools.ToolDefinition) *tools.Registry {
	reg := tools.NewRegistry()
	// delegate_to_crew is always registered (available to all crew).
	reg.Register(&tools.DelegateTool{})
	for _, t := range tt {
		reg.Register(t)
	}
	return reg
}

func newTestOrchestrator(t *testing.T, toolReg *tools.Registry, responses ...*anthropic.Message) (*orchestrator.Orchestrator, *mockClaudeClient) {
	t.Helper()
	reg := newTestRegistry(t)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	if len(responses) == 0 {
		responses = []*anthropic.Message{textResponse("Aye, that'll hold.")}
	}
	mock := &mockClaudeClient{responses: responses}
	return orchestrator.NewWithClient(reg, mgr, toolReg, mock), mock
}

// newTestOrchestratorWithMock is like newTestOrchestrator but uses a
// pre-built mock — needed for recovery tests that configure per-call
// errors via callErrors.
func newTestOrchestratorWithMock(t *testing.T, toolReg *tools.Registry, mock *mockClaudeClient) *orchestrator.Orchestrator {
	t.Helper()
	reg := newTestRegistry(t)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	return orchestrator.NewWithClient(reg, mgr, toolReg, mock)
}

// =====================================================================
// Existing tests (updated for new constructor)
// =====================================================================

func TestNewCreatesOrchestrator(t *testing.T) {
	defer testutil.VerifyNone(t)
	reg := newTestRegistry(t)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	toolReg := newToolRegistry()
	o := orchestrator.New(reg, mgr, toolReg, "sk-test-key")
	if o == nil {
		t.Fatal("expected non-nil orchestrator")
	}
}

func TestRouteDefault(t *testing.T) {
	o, _ := newTestOrchestrator(t, newToolRegistry())
	c := o.Route("")
	if c.ID() != "maren" {
		t.Errorf("expected default crew maren, got %s", c.ID())
	}
}

func TestRouteNamedCrew(t *testing.T) {
	o, _ := newTestOrchestrator(t, newToolRegistry())
	c := o.Route("crest")
	if c.ID() != "crest" {
		t.Errorf("expected crest, got %s", c.ID())
	}
}

func TestRouteUnknownFallsToDefault(t *testing.T) {
	o, _ := newTestOrchestrator(t, newToolRegistry())
	c := o.Route("ghost")
	if c.ID() != "maren" {
		t.Errorf("expected fallback to maren, got %s", c.ID())
	}
}

func TestHandoffRequestRoutesCorrectly(t *testing.T) {
	o, _ := newTestOrchestrator(t, newToolRegistry())
	c := o.Route("crest")
	if c.ID() != "crest" {
		t.Errorf("expected crest on handoff, got %s", c.ID())
	}
}

func TestHandleCallsClaude(t *testing.T) {
	o, mock := newTestOrchestrator(t, newToolRegistry())
	responses, err := o.Handle(t.Context(), "!room:server", "hull check", "")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Text != "Aye, that'll hold." {
		t.Errorf("unexpected response text: %q", responses[0].Text)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 API call, got %d", len(mock.calls))
	}
}

func TestHandleFramesPrefixAndCapsLength(t *testing.T) {
	o, mock := newTestOrchestrator(t, newToolRegistry())
	long := strings.Repeat("x", 1500)
	_, err := o.Handle(t.Context(), "!room:server", long, "")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	call := mock.calls[0]
	if len(call.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(call.Messages))
	}
	body := call.Messages[0].Content[0].OfText.Text
	if !strings.HasPrefix(body, "The Captain says: ") {
		t.Errorf("expected captain prefix, got: %q", body[:40])
	}
	if len(body) > len("The Captain says: ")+1000 {
		t.Errorf("message not capped: len=%d", len(body))
	}
}

func TestHandleContextBufferPassedToClaude(t *testing.T) {
	o, mock := newTestOrchestrator(t, newToolRegistry())
	_, err := o.Handle(t.Context(), "!room:server", "hello", "")
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}
	_, err = o.Handle(t.Context(), "!room:server", "follow up", "")
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}
	call := mock.calls[1]
	if len(call.Messages) < 3 {
		t.Errorf("expected history in messages (want ≥3), got %d", len(call.Messages))
	}
}

func TestHandleAPIErrorPropagated(t *testing.T) {
	reg := newTestRegistry(t)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	toolReg := newToolRegistry()
	mock := &mockClaudeClient{responses: []*anthropic.Message{textResponse("")}, err: errors.New("rate limited")}
	o := orchestrator.NewWithClient(reg, mgr, toolReg, mock)

	_, err := o.Handle(t.Context(), "!room:server", "hello", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected rate limited in error, got: %v", err)
	}
}

func TestHandleErrorsOnEmptyTextResponse(t *testing.T) {
	reg := newTestRegistry(t)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	toolReg := newToolRegistry()
	mock := &mockClaudeClient{responses: []*anthropic.Message{{
		StopReason: anthropic.StopReasonEndTurn,
		Content:    []anthropic.ContentBlockUnion{},
	}}}
	o := orchestrator.NewWithClient(reg, mgr, toolReg, mock)
	_, err := o.Handle(t.Context(), "!room:server", "hello", "")
	if err == nil {
		t.Fatal("expected error for empty response, got nil")
	}
}

func TestHandleRoutesToCrestWhenRequested(t *testing.T) {
	o, mock := newTestOrchestrator(t, newToolRegistry())
	responses, err := o.Handle(t.Context(), "!room:server", "check inbox", "crest")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].CrewID != "crest" {
		t.Errorf("expected crew crest, got %s", responses[0].CrewID)
	}
	call := mock.calls[0]
	if !strings.Contains(call.System[0].Text, "Crest") {
		t.Errorf("expected Crest system prompt, got: %q", call.System[0].Text[:50])
	}
}

// =====================================================================
// Tool-use loop tests
// =====================================================================

func TestToolUseSingleRoundTrip(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	// Claude requests echo_tool, then responds with text.
	o, mock := newTestOrchestrator(t, toolReg,
		toolUseResponse("tu_1", "echo_tool", json.RawMessage(`{"msg":"ping"}`)),
		textResponse("Echo says: ping"),
	)

	responses, err := o.Handle(t.Context(), "!room:server", "test echo", "crest")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].Text != "Echo says: ping" {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}
	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(mock.calls))
	}

	// Second call should contain tool_result in messages.
	secondCall := mock.calls[1]
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	if lastMsg.Content[0].OfToolResult == nil {
		t.Fatal("expected tool_result in second call's last message")
	}
	toolResult := lastMsg.Content[0].OfToolResult
	if toolResult.ToolUseID != "tu_1" {
		t.Errorf("expected tool_use_id tu_1, got %s", toolResult.ToolUseID)
	}
}

func TestToolUseMultipleRoundTrips(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	// Claude requests tool twice, then responds.
	o, mock := newTestOrchestrator(t, toolReg,
		toolUseResponse("tu_1", "echo_tool", json.RawMessage(`{"msg":"first"}`)),
		toolUseResponse("tu_2", "echo_tool", json.RawMessage(`{"msg":"second"}`)),
		textResponse("Done with both."),
	)

	responses, err := o.Handle(t.Context(), "!room:server", "test", "crest")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].Text != "Done with both." {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}
	if len(mock.calls) != 3 {
		t.Fatalf("expected 3 API calls (1 initial + 2 tool rounds), got %d", len(mock.calls))
	}
}

func TestToolUseMultipleToolsInOneResponse(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	// Claude requests two tools in one response.
	o, mock := newTestOrchestrator(t, toolReg,
		multiToolUseResponse(
			struct {
				ID    string
				Name  string
				Input json.RawMessage
			}{"tu_1", "echo_tool", json.RawMessage(`{"msg":"a"}`)},
			struct {
				ID    string
				Name  string
				Input json.RawMessage
			}{"tu_2", "echo_tool", json.RawMessage(`{"msg":"b"}`)},
		),
		textResponse("Got both echoes."),
	)

	responses, err := o.Handle(t.Context(), "!room:server", "test", "crest")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].Text != "Got both echoes." {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}
	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(mock.calls))
	}

	// Second call should have 2 tool_result blocks.
	secondCall := mock.calls[1]
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	if len(lastMsg.Content) != 2 {
		t.Fatalf("expected 2 tool_result blocks, got %d", len(lastMsg.Content))
	}
}

func TestToolUseToolError(t *testing.T) {
	toolReg := newToolRegistry(&failTool{})

	// Crew YAML doesn't declare fail_tool for crest, so use a custom YAML.
	crewYAML := `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [fail_tool]
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
`
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(crewYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := crew.Load(f)
	if err != nil {
		t.Fatal(err)
	}

	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	mock := &mockClaudeClient{responses: []*anthropic.Message{
		toolUseResponse("tu_1", "fail_tool", json.RawMessage(`{}`)),
		textResponse("Tool failed, but I handled it."),
	}}
	o := orchestrator.NewWithClient(reg, mgr, toolReg, mock)

	responses, err := o.Handle(t.Context(), "!room:server", "break it", "maren")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].Text != "Tool failed, but I handled it." {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}

	// Verify the tool_result was sent with isError=true.
	secondCall := mock.calls[1]
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	toolResult := lastMsg.Content[0].OfToolResult
	if toolResult.IsError.Value != true {
		t.Error("expected isError=true on tool result")
	}
}

func TestToolUseUnknownToolReturnsError(t *testing.T) {
	// Crew declares ghost_tool in allowlist, but it's not registered in the
	// tool registry. This bypasses the allowlist check and hits the
	// Registry.Execute "unknown tool" path.
	toolReg := newToolRegistry() // No tools registered.

	crewYAML := `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [ghost_tool]
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
`
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(crewYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := crew.Load(f)
	if err != nil {
		t.Fatal(err)
	}

	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	mock := &mockClaudeClient{responses: []*anthropic.Message{
		toolUseResponse("tu_1", "ghost_tool", json.RawMessage(`{}`)),
		textResponse("That tool doesn't exist."),
	}}
	o := orchestrator.NewWithClient(reg, mgr, toolReg, mock)

	responses, err := o.Handle(t.Context(), "!room:server", "test", "maren")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].Text != "That tool doesn't exist." {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}

	// Verify isError=true with "unknown tool" message from Registry.Execute.
	secondCall := mock.calls[1]
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	toolResult := lastMsg.Content[0].OfToolResult
	if toolResult.IsError.Value != true {
		t.Error("expected isError=true for unknown tool")
	}
	resultText := toolResult.Content[0].OfText.Text
	if !strings.Contains(resultText, "unknown tool") {
		t.Errorf("expected 'unknown tool' in result, got: %q", resultText)
	}
}

func TestToolUseCrewAllowlistEnforced(t *testing.T) {
	// Register echo_tool AND fail_tool, but crest only declares echo_tool.
	// Claude asks for fail_tool — should be rejected by allowlist.
	toolReg := newToolRegistry(&echoTool{}, &failTool{})

	o, mock := newTestOrchestrator(t, toolReg,
		toolUseResponse("tu_1", "fail_tool", json.RawMessage(`{}`)),
		textResponse("Tool not allowed."),
	)

	responses, err := o.Handle(t.Context(), "!room:server", "try fail_tool", "crest")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].Text != "Tool not allowed." {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}

	// Verify isError=true with "not allowed" message.
	secondCall := mock.calls[1]
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	toolResult := lastMsg.Content[0].OfToolResult
	if toolResult.IsError.Value != true {
		t.Error("expected isError=true for disallowed tool")
	}
	resultText := toolResult.Content[0].OfText.Text
	if !strings.Contains(resultText, "not allowed") {
		t.Errorf("expected 'not allowed' in result, got: %q", resultText)
	}
}

func TestToolUseMaxIterationsExceeded(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	// Claude keeps requesting tools forever (mock repeats last response).
	o, _ := newTestOrchestrator(t, toolReg,
		toolUseResponse("tu_1", "echo_tool", json.RawMessage(`{"msg":"loop"}`)),
	)

	_, err := o.Handle(t.Context(), "!room:server", "infinite loop", "crest")
	if err == nil {
		t.Fatal("expected error for max iterations exceeded")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Errorf("expected 'exceeded' in error, got: %v", err)
	}
}

func TestToolUseOutputTruncated(t *testing.T) {
	toolReg := newToolRegistry(&largeTool{})

	crewYAML := `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [large_tool]
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
`
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(crewYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := crew.Load(f)
	if err != nil {
		t.Fatal(err)
	}

	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	mock := &mockClaudeClient{responses: []*anthropic.Message{
		toolUseResponse("tu_1", "large_tool", json.RawMessage(`{}`)),
		textResponse("Got truncated output."),
	}}
	o := orchestrator.NewWithClient(reg, mgr, toolReg, mock)

	responses, err := o.Handle(t.Context(), "!room:server", "big output", "maren")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].Text != "Got truncated output." {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}

	// Verify the tool result was truncated.
	secondCall := mock.calls[1]
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	toolResult := lastMsg.Content[0].OfToolResult
	resultText := toolResult.Content[0].OfText.Text
	if !strings.HasSuffix(resultText, "[truncated]") {
		t.Errorf("expected [truncated] suffix, got last 20 chars: %q", resultText[len(resultText)-20:])
	}
	// 4096 + len("\n[truncated]") = 4108
	if len(resultText) > 4120 {
		t.Errorf("result too long after truncation: %d", len(resultText))
	}
}

func TestToolUseToolsPassedToClaudeAPI(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	o, mock := newTestOrchestrator(t, toolReg)
	// Crest has echo_tool declared — it should be passed to Claude.
	_, err := o.Handle(t.Context(), "!room:server", "test", "crest")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	call := mock.calls[0]
	if len(call.Tools) != 2 {
		t.Fatalf("expected 2 tools passed to Claude (delegate_to_crew + echo_tool), got %d", len(call.Tools))
	}
	toolNames := make(map[string]bool)
	for _, tool := range call.Tools {
		toolNames[tool.OfTool.Name] = true
	}
	if !toolNames["echo_tool"] {
		t.Error("expected echo_tool in tools")
	}
	if !toolNames["delegate_to_crew"] {
		t.Error("expected delegate_to_crew in tools")
	}
}

func TestToolUseNoToolsForCrewWithoutTools(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	o, mock := newTestOrchestrator(t, toolReg)
	// Maren has tools: [] — no tools should be passed.
	_, err := o.Handle(t.Context(), "!room:server", "test", "maren")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	call := mock.calls[0]
	// Maren has tools: [delegate_to_crew] — only the delegation tool.
	if len(call.Tools) != 1 {
		t.Fatalf("expected 1 tool (delegate_to_crew) for maren, got %d", len(call.Tools))
	}
	if call.Tools[0].OfTool.Name != "delegate_to_crew" {
		t.Errorf("expected delegate_to_crew, got %q", call.Tools[0].OfTool.Name)
	}
}

func TestToolUseContextBufferIncludesToolTurns(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	o, mock := newTestOrchestrator(t, toolReg,
		toolUseResponse("tu_1", "echo_tool", json.RawMessage(`{"msg":"ping"}`)),
		textResponse("Pong."),
	)

	// First message with tool use.
	_, err := o.Handle(t.Context(), "!room:server", "test", "crest")
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}

	// Second message — history should include tool turns.
	_, err = o.Handle(t.Context(), "!room:server", "follow up", "crest")
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}

	// Third API call (second Handle's first call) should have history.
	// Messages: [user1, assistant(tool_use), user(tool_result), assistant(text), user2]
	thirdCall := mock.calls[2]
	if len(thirdCall.Messages) < 5 {
		t.Errorf("expected ≥5 messages in history (tool turns included), got %d", len(thirdCall.Messages))
	}
}

func TestToolUseTextAndToolUseInSameResponse(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	// Claude sends text AND tool_use in the same response.
	o, mock := newTestOrchestrator(t, toolReg,
		toolUseWithTextResponse("Let me check...", "tu_1", "echo_tool", json.RawMessage(`{"msg":"hi"}`)),
		textResponse("All done."),
	)

	responses, err := o.Handle(t.Context(), "!room:server", "test", "crest")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].Text != "All done." {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}
	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(mock.calls))
	}

	// The assistant message in the second call should have both text and tool_use blocks.
	secondCall := mock.calls[1]
	// Messages: [user, assistant(text+tool_use), user(tool_result)]
	assistantMsg := secondCall.Messages[len(secondCall.Messages)-2]
	if len(assistantMsg.Content) != 2 {
		t.Errorf("expected 2 blocks in assistant message (text + tool_use), got %d", len(assistantMsg.Content))
	}
}

func TestToolUseAPIErrorDuringLoop(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	reg := newTestRegistry(t)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)

	mock := &mockClaudeClient{
		responses: []*anthropic.Message{
			toolUseResponse("tu_1", "echo_tool", json.RawMessage(`{"msg":"ok"}`)),
		},
	}
	failOnSecond := &apiFailOnCallN{
		inner: mock,
		failN: 1, // 0-indexed: fail on second call
	}

	o := orchestrator.NewWithClient(reg, mgr, toolReg, failOnSecond)

	_, err := o.Handle(t.Context(), "!room:server", "test", "crest")
	if err == nil {
		t.Fatal("expected error from API failure during tool loop")
	}
	if !strings.Contains(err.Error(), "api exploded") {
		t.Errorf("unexpected error: %v", err)
	}
}

// apiFailOnCallN fails on the Nth call (0-indexed).
type apiFailOnCallN struct {
	inner *mockClaudeClient
	failN int
	count int
}

func (a *apiFailOnCallN) New(ctx context.Context, body anthropic.MessageNewParams, opts ...option.RequestOption) (*anthropic.Message, error) {
	if a.count == a.failN {
		a.count++
		return nil, fmt.Errorf("api exploded")
	}
	a.count++
	return a.inner.New(ctx, body, opts...)
}

// =====================================================================
// Delegation tests
// =====================================================================

func TestDelegationHappyPath(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	// Maren uses delegate_to_crew → Crest responds.
	delegateInput := json.RawMessage(`{"crew":"crest","context":"Check the inbox"}`)
	o, _ := newTestOrchestrator(t, toolReg,
		// Maren's response: text + delegate tool use
		toolUseWithTextResponse("Aye, hull's sound. But Crest should check signals.",
			"tu_1", "delegate_to_crew", delegateInput),
		// Crest's response (second Handle call):
		textResponse("Signal received. Checking the inbox now."),
	)

	responses, err := o.Handle(t.Context(), "!room:server", "status report", "maren")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses (maren + crest), got %d", len(responses))
	}
	if responses[0].CrewID != "maren" {
		t.Errorf("first response crew = %q, want maren", responses[0].CrewID)
	}
	if responses[0].Text != "Aye, hull's sound. But Crest should check signals." {
		t.Errorf("first response text = %q", responses[0].Text)
	}
	if responses[1].CrewID != "crest" {
		t.Errorf("second response crew = %q, want crest", responses[1].CrewID)
	}
	if responses[1].Text != "Signal received. Checking the inbox now." {
		t.Errorf("second response text = %q", responses[1].Text)
	}
}

func TestDelegationMaxDepthExceeded(t *testing.T) {
	toolReg := newToolRegistry(&echoTool{})

	// A→B→C: three delegations, but max depth is 2 so C's delegation is ignored.
	delegateToCrest := json.RawMessage(`{"crew":"crest","context":"Your turn"}`)
	delegateToMaren := json.RawMessage(`{"crew":"maren","context":"Back to you"}`)

	o, _ := newTestOrchestrator(t, toolReg,
		// depth 0: Maren delegates to Crest
		toolUseWithTextResponse("Maren says hi.",
			"tu_1", "delegate_to_crew", delegateToCrest),
		// depth 1: Crest delegates to Maren
		toolUseWithTextResponse("Crest says hi.",
			"tu_2", "delegate_to_crew", delegateToMaren),
		// depth 2: Maren tries to delegate again — should be ignored (at max depth)
		toolUseWithTextResponse("Maren again.",
			"tu_3", "delegate_to_crew", delegateToCrest),
	)

	responses, err := o.Handle(t.Context(), "!room:server", "ping pong", "maren")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// depth 0: Maren, depth 1: Crest, depth 2: Maren (stops here, no further delegation)
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses (depth 0,1,2), got %d", len(responses))
	}
	if responses[0].CrewID != "maren" {
		t.Errorf("response[0] crew = %q, want maren", responses[0].CrewID)
	}
	if responses[1].CrewID != "crest" {
		t.Errorf("response[1] crew = %q, want crest", responses[1].CrewID)
	}
	if responses[2].CrewID != "maren" {
		t.Errorf("response[2] crew = %q, want maren", responses[2].CrewID)
	}
}

func TestDelegationToUnknownCrewFallsToDefault(t *testing.T) {
	toolReg := newToolRegistry()

	// Maren delegates to "ghost" — unknown crew falls back to default (maren).
	delegateInput := json.RawMessage(`{"crew":"ghost","context":"Who are you?"}`)
	o, _ := newTestOrchestrator(t, toolReg,
		toolUseWithTextResponse("Let me ask ghost.",
			"tu_1", "delegate_to_crew", delegateInput),
		textResponse("I'm the default, maren."),
	)

	responses, err := o.Handle(t.Context(), "!room:server", "test", "maren")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
	// Delegation to unknown falls back to default (maren).
	if responses[1].CrewID != "maren" {
		t.Errorf("delegated response crew = %q, want maren (default fallback)", responses[1].CrewID)
	}
}

// slowTool sleeps until context is cancelled, simulating a tool that exceeds
// the sandbox timeout.
type slowTool struct{}

func (s *slowTool) Name() string        { return "slow_tool" }
func (s *slowTool) Description() string { return "sleeps forever" }
func (s *slowTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
}
func (s *slowTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestToolUseSandboxTimeoutThroughHandle(t *testing.T) {
	toolReg := newToolRegistry(&slowTool{})

	crewYAML := `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [delegate_to_crew, slow_tool]
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
`
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(crewYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := crew.Load(f)
	if err != nil {
		t.Fatal(err)
	}

	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	mock := &mockClaudeClient{responses: []*anthropic.Message{
		toolUseResponse("tu_1", "slow_tool", json.RawMessage(`{}`)),
		textResponse("Tool timed out, but I recovered."),
	}}
	o := orchestrator.NewWithClient(reg, mgr, toolReg, mock)
	cfg := tools.DefaultSandboxConfig()
	cfg.Timeout = 50 * time.Millisecond
	o.SetSandboxConfig(cfg)

	responses, err := o.Handle(t.Context(), "!room:server", "run slow tool", "maren")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(responses) == 0 {
		t.Fatal("Handle returned no responses")
	}
	if responses[0].Text != "Tool timed out, but I recovered." {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}

	// Verify the tool_result was sent with isError=true and contains timeout.
	if len(mock.calls) < 2 {
		t.Fatalf("expected at least 2 Claude calls, got %d", len(mock.calls))
	}
	secondCall := mock.calls[1]
	if len(secondCall.Messages) == 0 {
		t.Fatal("expected at least 1 message in second Claude call")
	}
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	if len(lastMsg.Content) == 0 {
		t.Fatal("expected at least 1 content block in last message")
	}
	toolResult := lastMsg.Content[0].OfToolResult
	if toolResult == nil {
		t.Fatal("expected tool_result content in last message")
	}
	if toolResult.IsError.Value != true {
		t.Error("expected isError=true for timed-out tool")
	}
	if len(toolResult.Content) == 0 || toolResult.Content[0].OfText == nil {
		t.Fatal("expected text content in tool_result")
	}
	resultText := toolResult.Content[0].OfText.Text
	if !strings.Contains(resultText, "context deadline exceeded") {
		t.Errorf("expected 'context deadline exceeded' in result, got: %q", resultText)
	}
}

// --- 400 auto-recovery tests (bridge#100) ---

// errOrphanedToolResult mimics the anthropic-sdk-go Error().
// The real SDK error includes the HTTP method, URL, status code, and raw JSON
// body. isOrphanedToolResultError matches on "400" + "unexpected tool_use_id".
var errOrphanedToolResult = fmt.Errorf(
	`POST "https://api.anthropic.com/v1/messages": 400 Bad Request {"type":"error","error":{"type":"invalid_request_error","message":"messages.0.content.0: unexpected tool_use_id found in tool_result blocks: toolu_01ABC"}}`,
)

func TestHandle400OrphanedRetrySucceeds(t *testing.T) {
	mock := &mockClaudeClient{
		// Call 0: 400 orphaned tool_result error.
		// Call 1 (retry): success text response.
		callErrors: []error{errOrphanedToolResult, nil},
		responses:  []*anthropic.Message{textResponse("Recovered, Captain.")},
	}
	toolReg := tools.NewRegistry()
	orch := newTestOrchestratorWithMock(t, toolReg, mock)

	responses, err := orch.Handle(t.Context(), "!room:server", "hull check", "")
	if err != nil {
		t.Fatalf("expected recovery, got error: %v", err)
	}
	if len(responses) == 0 || responses[0].Text != "Recovered, Captain." {
		t.Errorf("unexpected response: %+v", responses)
	}
	// Should have called the API exactly twice: initial (400) + retry (success).
	if len(mock.calls) != 2 {
		t.Errorf("expected 2 API calls (initial + retry), got %d", len(mock.calls))
	}
	// Retry should have only the fresh user message (buffer was cleared).
	retryMsgs := mock.calls[1].Messages
	if len(retryMsgs) != 1 {
		t.Errorf("expected 1 message in retry (fresh user only), got %d", len(retryMsgs))
	}
}

func TestHandle400DifferentErrorNoRetry(t *testing.T) {
	differentError := fmt.Errorf(`POST "https://api.anthropic.com/v1/messages": 400 Bad Request {"type":"error","error":{"type":"invalid_request_error","message":"max_tokens must be positive"}}`)
	mock := &mockClaudeClient{
		err: differentError,
	}
	toolReg := tools.NewRegistry()
	orch := newTestOrchestratorWithMock(t, toolReg, mock)

	_, err := orch.Handle(t.Context(), "!room:server", "hull check", "")
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if !strings.Contains(err.Error(), "max_tokens") {
		t.Errorf("expected original error, got: %v", err)
	}
	// Should have called exactly once — no retry.
	if len(mock.calls) != 1 {
		t.Errorf("expected 1 API call (no retry), got %d", len(mock.calls))
	}
}

func TestHandle400OrphanedRetryAlsoFails(t *testing.T) {
	mock := &mockClaudeClient{
		// Both calls fail with the same orphaned error.
		callErrors: []error{errOrphanedToolResult, errOrphanedToolResult},
	}
	toolReg := tools.NewRegistry()
	orch := newTestOrchestratorWithMock(t, toolReg, mock)

	_, err := orch.Handle(t.Context(), "!room:server", "hull check", "")
	if err == nil {
		t.Fatal("expected error after retry failure")
	}
	if !strings.Contains(err.Error(), "retry after buffer clear") {
		t.Errorf("expected retry-failure error, got: %v", err)
	}
	// Two calls: initial + one retry.
	if len(mock.calls) != 2 {
		t.Errorf("expected 2 API calls, got %d", len(mock.calls))
	}
}

func TestHandle400MidLoopNoRecovery(t *testing.T) {
	// First call: tool_use response (iteration 0 succeeds).
	// Second call: 400 orphaned error (iteration 1 — mid-loop).
	// Mid-loop recovery is NOT attempted.
	mock := &mockClaudeClient{
		callErrors: []error{nil, errOrphanedToolResult},
		responses:  []*anthropic.Message{toolUseResponse("tu_1", "echo_tool", json.RawMessage(`{"msg":"hi"}`))},
	}
	toolReg := tools.NewRegistry()
	toolReg.Register(&echoTool{})
	orch := newTestOrchestratorWithMock(t, toolReg, mock)

	_, err := orch.Handle(t.Context(), "!room:server", "hull check", "maren")
	if err == nil {
		t.Fatal("expected error from mid-loop 400")
	}
	if strings.Contains(err.Error(), "retry") {
		t.Errorf("mid-loop 400 should NOT retry, got: %v", err)
	}
	// Two calls: iteration 0 (success) + iteration 1 (400, no retry).
	if len(mock.calls) != 2 {
		t.Errorf("expected 2 API calls, got %d", len(mock.calls))
	}
}

// =====================================================================
// L2 enforcement roster (#154)
//
// Two of the four L2 tests for the #148 epic live here. Companion tests:
//   - internal/crew/registry_test.go::TestChipsSystemPromptContainsOperatorIntentRule
//     — Compose output carries the Operator Intent rule and worked example
//   - internal/tools/chips/chips_test.go::TestProductionChipsRegistryHasNoMergeTools
//     — gh_pr_merge / gh_pr_ready are not registered
// =====================================================================

const chipsTestCrewYAML = `
default_crew: chips
crew:
  chips:
    name: "Chips"
    role: "The Carpenter"
    model: "claude-sonnet-4-6"
    verbosity: log-entry
    tools: [delegate_to_crew, gh_pr_list, test_mutation]
    voice:
      model: TBD
      announces_as: "Chips:"
    system_prompt: "You are Chips. Respond in {verbosity}"
`

// mutationTool is a test-only ToolDefinition that opts into MutationAware
// so the orchestrator marks it via SandboxMeta.Mutation. Used by the
// pending-confirmation exception test below.
type mutationTool struct {
	executed bool
}

func (m *mutationTool) Name() string        { return "test_mutation" }
func (m *mutationTool) Description() string { return "test-only mutating tool" }
func (m *mutationTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
}
func (m *mutationTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	m.executed = true
	return "mutation executed", nil
}
func (m *mutationTool) Mutation() bool { return true }

// newChipsOrchestrator loads a chips-defaulted test crew and wires the
// orchestrator with the supplied mock-response sequence.
func newChipsOrchestrator(t *testing.T, toolReg *tools.Registry, responses ...*anthropic.Message) (*orchestrator.Orchestrator, *mockClaudeClient) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(chipsTestCrewYAML), 0o600); err != nil {
		t.Fatalf("write crew yaml: %v", err)
	}
	reg, err := crew.Load(f)
	if err != nil {
		t.Fatalf("load crew: %v", err)
	}
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	mock := &mockClaudeClient{responses: responses}
	return orchestrator.NewWithClient(reg, mgr, toolReg, mock), mock
}

// TestChipsRefusesNonAllowlistedRepo drives the allowlist refusal path
// for chips's gh_pr_list against an off-allowlist repo. The repo
// allowlist check fires before exec, so the tool returns an error which
// the orchestrator wraps as a tool_result with is_error=true and the
// "is not in the allowed list" substring. Exec must not be called.
func TestChipsRefusesNonAllowlistedRepo(t *testing.T) {
	defer testutil.VerifyNone(t)
	repoAllow := chipstools.ParseRepoAllowlist("klazomenai/bridge")
	stubExec := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		t.Fatal("stub exec called — allowlist check should have refused before exec")
		return nil, nil
	}
	toolReg := tools.NewRegistry()
	toolReg.Register(&tools.DelegateTool{})
	toolReg.Register(chipstools.NewGHPRListTool(stubExec, repoAllow, "test-token"))

	o, mock := newChipsOrchestrator(t, toolReg,
		toolUseResponse("tu_1", "gh_pr_list", json.RawMessage(`{"org":"microsoft","repo":"vscode"}`)),
		textResponse("microsoft/vscode is not in our allowlist, Captain."),
	)

	if _, err := o.Handle(t.Context(), "!room:server", "list open PRs on microsoft/vscode", "chips"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 API calls (initial + tool-loop continuation), got %d", len(mock.calls))
	}

	secondCall := mock.calls[1]
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	tr := lastMsg.Content[0].OfToolResult
	if tr == nil {
		t.Fatal("expected tool_result in second call's last message")
	}
	if !tr.IsError.Value {
		t.Error("expected tool_result is_error=true on allowlist refusal")
	}
	resultText := tr.Content[0].OfText.Text
	if !strings.Contains(resultText, "is not in the allowed list") {
		t.Errorf("tool_result missing allowlist-refusal substring, got: %q", resultText)
	}
}

// TestPendingConfirmationExceptionAccepts drives the pending-confirmation
// exception path from _universal.md: operator proposes a mutation, the
// orchestrator surfaces a confirm prompt with no tool_use, the operator
// says "yes", and the mutation tool then fires without the orchestrator
// refusing on the bare "yes" turn. Both operator turns share the same
// room and therefore the same context buffer, so the prior proposal is
// visible to Claude on the confirmation turn.
//
// Caveat: the mock returns exactly what the test scripts. This proves
// the multi-turn machinery (context buffer + tool-loop) threads the
// confirmation through to tool execution, not that Claude itself would
// refuse the bare "yes" without the prior turn. The prompt-content
// guarantee (chips sees the Operator Intent rule + worked example) is
// covered in the companion crew test.
func TestPendingConfirmationExceptionAccepts(t *testing.T) {
	defer testutil.VerifyNone(t)
	mt := &mutationTool{}
	toolReg := tools.NewRegistry()
	toolReg.Register(&tools.DelegateTool{})
	toolReg.Register(mt)

	o, mock := newChipsOrchestrator(t, toolReg,
		// Turn 1: chips proposes the action without committing.
		textResponse("That's a high-risk mutation — confirm close of issue #99? (yes/no)"),
		// Turn 2 (after "yes"): chips fires the mutation tool.
		toolUseResponse("tu_1", "test_mutation", json.RawMessage(`{}`)),
		// Turn 2 follow-up after the tool_result.
		textResponse("Issue #99 closed."),
	)

	const room = "!confirm:server"

	// Turn 1: proposal.
	resp, err := o.Handle(t.Context(), room, "close issue #99", "chips")
	if err != nil {
		t.Fatalf("turn 1 Handle: %v", err)
	}
	if len(resp) != 1 || !strings.Contains(strings.ToLower(resp[0].Text), "confirm") {
		t.Fatalf("turn 1: expected confirm prompt, got %+v", resp)
	}
	if mt.executed {
		t.Error("turn 1: mutation tool executed before confirmation")
	}

	// Turn 2: confirmation.
	resp, err = o.Handle(t.Context(), room, "yes", "chips")
	if err != nil {
		t.Fatalf("turn 2 Handle: %v", err)
	}
	if !mt.executed {
		t.Error("turn 2: mutation tool was not executed after confirmation")
	}
	if len(resp) != 1 || resp[0].Text != "Issue #99 closed." {
		t.Fatalf("turn 2: expected close-confirmation text, got %+v", resp)
	}

	// Verify the tool_result on the post-tool turn was NOT marked as
	// an error — the orchestrator threaded the confirmation turn
	// through to tool execution rather than refusing.
	lastCall := mock.calls[len(mock.calls)-1]
	lastMsg := lastCall.Messages[len(lastCall.Messages)-1]
	tr := lastMsg.Content[0].OfToolResult
	if tr == nil {
		t.Fatal("expected tool_result in final mock call")
	}
	if tr.IsError.Value {
		t.Errorf("tool_result unexpectedly marked is_error=true: %+v", tr.Content)
	}

	// Sanity: 3 mock calls total — turn 1 (1) + turn 2 (initial + post-tool, 2).
	if got := len(mock.calls); got != 3 {
		t.Errorf("expected 3 total mock calls (turn 1 + turn 2 with tool loop), got %d", got)
	}
}

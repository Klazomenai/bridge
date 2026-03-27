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
	"klazomenai/bridge/internal/tools"
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
type mockClaudeClient struct {
	responses []*anthropic.Message // returned in order; last one repeats if exhausted
	err       error
	calls     []anthropic.MessageNewParams
	callIndex int
}

func (m *mockClaudeClient) New(_ context.Context, body anthropic.MessageNewParams, _ ...option.RequestOption) (*anthropic.Message, error) {
	m.calls = append(m.calls, body)
	if m.err != nil {
		return nil, m.err
	}
	idx := m.callIndex
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callIndex++
	return m.responses[idx], nil
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

// =====================================================================
// Existing tests (updated for new constructor)
// =====================================================================

func TestNewCreatesOrchestrator(t *testing.T) {
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
	o.SetSandboxConfig(tools.SandboxConfig{Timeout: 50 * time.Millisecond, MaxOutputLen: 4096})

	responses, err := o.Handle(t.Context(), "!room:server", "run slow tool", "maren")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if responses[0].Text != "Tool timed out, but I recovered." {
		t.Errorf("unexpected text: %q", responses[0].Text)
	}

	// Verify the tool_result was sent with isError=true and contains timeout.
	secondCall := mock.calls[1]
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	toolResult := lastMsg.Content[0].OfToolResult
	if toolResult.IsError.Value != true {
		t.Error("expected isError=true for timed-out tool")
	}
	resultText := toolResult.Content[0].OfText.Text
	if !strings.Contains(resultText, "context deadline exceeded") {
		t.Errorf("expected 'context deadline exceeded' in result, got: %q", resultText)
	}
}

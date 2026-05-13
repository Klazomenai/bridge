package orchestrator_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	ctxbuf "klazomenai/bridge/internal/context"
	"klazomenai/bridge/internal/crew"
	"klazomenai/bridge/internal/orchestrator"
	"klazomenai/bridge/internal/tools"
	"klazomenai/bridge/internal/tools/redact"
)

// captureSlogForOrch routes redact's "sanitiser_redaction" emissions
// to a buffer via redact.SetLogger so per-call-site tests can prove
// the orchestrator floor actually invoked the helper at each
// NewToolResultBlock site. The orchestrator's own slog calls go to
// slog.Default and are NOT captured here — the buffer holds only
// floor emissions plus any panic-recovery line, keeping assertions
// scoped.
func captureSlogForOrch(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	restore := redact.SetLogger(logger)
	return &buf, restore
}

// configurableOutputTool returns a fixed string regardless of input.
// Used by sanitiser-floor tests that need specific output shapes
// (planted tokens, multi-pattern payloads) without rewiring a
// production tool's mock behaviour.
type configurableOutputTool struct {
	name   string
	output string
}

func (t *configurableOutputTool) Name() string        { return t.name }
func (t *configurableOutputTool) Description() string { return "test tool returning configurable output" }
func (t *configurableOutputTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
}
func (t *configurableOutputTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return t.output, nil
}

// panicOnInfoHandler is a test-only slog.Handler that panics when
// asked to emit at slog.LevelInfo. Used to exercise the orchestrator
// floor's fail-closed contract: redact.SanitiseWith's per-pattern
// Info emission panics, the deferred recover catches it, emits the
// Error line via the same logger (which does NOT panic on Error),
// and returns SanitiserErrorReplacement. The orchestrator floor
// passes that replacement straight through to the tool_result.
type panicOnInfoHandler struct{}

func (h *panicOnInfoHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *panicOnInfoHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelInfo {
		panic("simulated sanitise handler panic on Info emission")
	}
	return nil
}

func (h *panicOnInfoHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *panicOnInfoHandler) WithGroup(_ string) slog.Handler      { return h }

// sanitiserTestOrchestrator wires up an orchestrator whose crew yaml
// authorises the supplied test tool, returning the orchestrator and
// the mock Claude client so callers can inspect what was sent back
// to the model in the second API call's tool_result block.
func sanitiserTestOrchestrator(t *testing.T, tool tools.ToolDefinition, responses ...*anthropic.Message) (*orchestrator.Orchestrator, *mockClaudeClient) {
	t.Helper()

	crewYAML := fmt.Sprintf(`
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [%s]
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
`, tool.Name())
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(crewYAML), 0o600); err != nil {
		t.Fatalf("write crew yaml: %v", err)
	}
	reg, err := crew.Load(f)
	if err != nil {
		t.Fatalf("load crew: %v", err)
	}
	toolReg := newToolRegistry(tool)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	mock := &mockClaudeClient{responses: responses}
	o := orchestrator.NewWithClient(reg, mgr, toolReg, mock)
	return o, mock
}

// extractToolResultText returns the text content of the tool_result
// block sent in the second API call (i.e. the result fed back to
// Claude after the first tool round). Fails the test if no
// tool_result is present.
func extractToolResultText(t *testing.T, mock *mockClaudeClient) string {
	t.Helper()
	if len(mock.calls) < 2 {
		t.Fatalf("expected at least 2 API calls, got %d", len(mock.calls))
	}
	secondCall := mock.calls[1]
	if len(secondCall.Messages) == 0 {
		t.Fatal("second call has no messages")
	}
	lastMsg := secondCall.Messages[len(secondCall.Messages)-1]
	if len(lastMsg.Content) == 0 || lastMsg.Content[0].OfToolResult == nil {
		t.Fatal("expected tool_result block in last message")
	}
	tr := lastMsg.Content[0].OfToolResult
	if len(tr.Content) == 0 || tr.Content[0].OfText == nil {
		t.Fatal("expected text content in tool_result")
	}
	return tr.Content[0].OfText.Text
}

// TestOrchestrator_AllToolResultsPassThroughSanitiser is the
// contractual test demanded by #83 AC10 / #129 AC: every tool_result
// content block routed through the orchestrator passes through the
// shared redact.Sanitise, regardless of whether the producing tool
// already sanitised its own output. The table covers a single-
// pattern case per default pattern shape PLUS a multi-pattern case,
// so a tool whose output bypasses the per-tool layer (new author,
// third-party integration) still has the floor as a backstop.
func TestOrchestrator_AllToolResultsPassThroughSanitiser(t *testing.T) {
	cases := []struct {
		name        string
		output      string
		mustNotHave string
		mustContain string
	}{
		{
			name:        "aws_access_key",
			output:      "comment: AKIATESTKEY012345678 planted by attacker",
			mustNotHave: "AKIATESTKEY012345678",
			mustContain: "AKIA…REDACTED",
		},
		{
			name:        "github_token",
			output:      "PAT leaked: ghp_" + strings.Repeat("A", 40) + " end",
			mustNotHave: strings.Repeat("A", 40),
			mustContain: "ghp_…REDACTED",
		},
		{
			name:        "openai_anthropic_key",
			output:      "claude key sk-ant-" + strings.Repeat("d", 40) + " in body",
			mustNotHave: strings.Repeat("d", 40),
			mustContain: "sk-…REDACTED",
		},
		{
			name:        "multi_pattern_payload",
			output:      "AWS=AKIATESTKEY012345678 GH=ghp_" + strings.Repeat("B", 40),
			mustNotHave: "AKIATESTKEY012345678",
			mustContain: "AKIA…REDACTED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := &configurableOutputTool{
				name:   "test_floor_" + tc.name,
				output: tc.output,
			}
			o, mock := sanitiserTestOrchestrator(t, tool,
				toolUseResponse("tu_1", tool.Name(), json.RawMessage(`{}`)),
				textResponse("ok"),
			)
			_, err := o.Handle(t.Context(), "!room:server", "test", "maren")
			if err != nil {
				t.Fatalf("Handle: %v", err)
			}
			content := extractToolResultText(t, mock)
			if strings.Contains(content, tc.mustNotHave) {
				t.Errorf("raw token leaked through orchestrator floor: %q", content)
			}
			if !strings.Contains(content, tc.mustContain) {
				t.Errorf("expected %q in tool_result content, got: %q",
					tc.mustContain, content)
			}
		})
	}
}

// TestOrchestrator_SanitiseIdempotentAtBoundary pins that content
// already redacted by a per-tool layer (e.g. chips returned
// `... AKIA…REDACTED ...`) passes through the floor unchanged. This
// is load-bearing because in steady state with per-tool sanitisers
// firing, the floor's input IS already-sanitised content — it must
// be a no-op rather than re-mangle the sentinels.
func TestOrchestrator_SanitiseIdempotentAtBoundary(t *testing.T) {
	preSanitised := "result: AKIA…REDACTED and ghp_…REDACTED already redacted upstream"
	out := orchestrator.SanitiseToolResultContentForTest("any_tool", preSanitised)
	if out != preSanitised {
		t.Errorf("pre-sanitised content altered at floor: %q → %q", preSanitised, out)
	}
}

// TestOrchestrator_SanitiseLengthCeilingAtBoundary pins that content
// exceeding MaxSanitiserInputBytes is truncated at the floor even
// when the tool layer's own MaxOutputLen cap is overridden or
// bypassed. The floor's cap is inherited from redact.SanitiseWith;
// this test exercises it via the direct helper because the default
// sandbox cap (4 096) sits well below MaxSanitiserInputBytes
// (65 536) and would otherwise mask the floor's truncation.
func TestOrchestrator_SanitiseLengthCeilingAtBoundary(t *testing.T) {
	oversized := strings.Repeat("a", redact.MaxSanitiserInputBytes+1024)
	out := orchestrator.SanitiseToolResultContentForTest("any_tool", oversized)
	if len(out) > redact.MaxSanitiserInputBytes {
		t.Errorf("orchestrator floor did not truncate oversized input: got %d bytes, cap %d",
			len(out), redact.MaxSanitiserInputBytes)
	}
}

// TestOrchestrator_FloorAppliedToAllowlistRefusalPath pins that the
// "tool not in crew allowlist" branch (the first NewToolResultBlock
// site in executeToolCalls) also runs through sanitiseToolResultContent.
// Constructed by having Claude request a tool whose Name is itself a
// token-shape string: the orchestrator-built error message
// `tool "AKIATESTKEY012345678" not allowed for this crew member`
// contains an AWS-shape literal, so the floor matches, redacts, and
// emits a `sanitiser_redaction` line tagged with that tool name. The
// assertions cover both the visible-content side (tool_result sent to
// Claude carries the sentinel, not the raw token) and the floor-
// emission side (the slog line proves the helper actually fired at
// this call site, not just that the code path doesn't crash).
func TestOrchestrator_FloorAppliedToAllowlistRefusalPath(t *testing.T) {
	buf, restore := captureSlogForOrch(t)
	defer restore()

	const leakyToolName = "AKIATESTKEY012345678"
	leakyTool := &configurableOutputTool{name: leakyToolName, output: "irrelevant"}

	// Maren's allowlist is [delegate_to_crew] only — leakyToolName
	// is registered but NOT authorised, so path 1 fires.
	crewYAML := `
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
`
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(crewYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := crew.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	toolReg := newToolRegistry(leakyTool)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	mock := &mockClaudeClient{responses: []*anthropic.Message{
		toolUseResponse("tu_1", leakyToolName, json.RawMessage(`{}`)),
		textResponse("Got the error."),
	}}
	o := orchestrator.NewWithClient(reg, mgr, toolReg, mock)

	if _, err := o.Handle(t.Context(), "!room:server", "try leaky", "maren"); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// (1) Floor emission proves the helper ran at the allowlist-refusal site.
	logOut := buf.String()
	if !strings.Contains(logOut, `"msg":"sanitiser_redaction"`) {
		t.Errorf("expected floor emission in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"layer":"orchestrator_floor"`) {
		t.Errorf("expected layer=orchestrator_floor in log, got: %s", logOut)
	}
	// The `tool` slog attribute is itself Sanitise'd before emission
	// — block.Name on this path is model-supplied and could itself
	// be a token shape, so the raw name MUST NOT surface in the
	// floor's own attribution log even though the content was
	// redacted (Copilot-flagged regression: without this sanitise,
	// the floor would leak in the log what it just redacted from the
	// tool_result content).
	if strings.Contains(logOut, leakyToolName) {
		t.Errorf("raw tool-name token surfaced in floor's own slog attribution: %s", logOut)
	}
	if !strings.Contains(logOut, `"tool":"AKIA…REDACTED"`) {
		t.Errorf("expected tool=AKIA…REDACTED in log (sanitised form), got: %s", logOut)
	}

	// (2) Tool-result content sent back to Claude carries the
	//     sentinel, not the raw token.
	content := extractToolResultText(t, mock)
	if strings.Contains(content, leakyToolName) {
		t.Errorf("raw tool-name token surfaced in tool_result: %q", content)
	}
	if !strings.Contains(content, "AKIA…REDACTED") {
		t.Errorf("expected AKIA…REDACTED in tool_result, got: %q", content)
	}
	// And isError=true is preserved (the floor doesn't touch the
	// error flag, only the content string).
	secondCall := mock.calls[1]
	tr := secondCall.Messages[len(secondCall.Messages)-1].Content[0].OfToolResult
	if !tr.IsError.Value {
		t.Error("expected isError=true on allowlist-refusal result")
	}
}

// TestOrchestrator_FloorAppliedToUnknownToolPath pins that the
// "unknown tool" branch (the second NewToolResultBlock site)
// constructs its error message through sanitiseToolResultContent.
// Setup: the crew's allowlist DECLARES a token-shape tool name, but
// that tool is NOT registered — so the allowlist check passes and
// the registry lookup fails, hitting path 2. The constructed error
// `tool error: unknown tool: "AKIATESTKEY012345678"` contains the
// AWS-shape literal which the floor redacts.
func TestOrchestrator_FloorAppliedToUnknownToolPath(t *testing.T) {
	buf, restore := captureSlogForOrch(t)
	defer restore()

	const leakyToolName = "AKIATESTKEY012345678"

	crewYAML := fmt.Sprintf(`
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [%s]
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
`, leakyToolName)
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(crewYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := crew.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	// Empty tool registry — the named tool is allowlisted but not registered.
	toolReg := newToolRegistry()
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	mock := &mockClaudeClient{responses: []*anthropic.Message{
		toolUseResponse("tu_1", leakyToolName, json.RawMessage(`{}`)),
		textResponse("Got the unknown-tool error."),
	}}
	o := orchestrator.NewWithClient(reg, mgr, toolReg, mock)

	if _, err := o.Handle(t.Context(), "!room:server", "try ghost", "maren"); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	logOut := buf.String()
	if !strings.Contains(logOut, `"msg":"sanitiser_redaction"`) {
		t.Errorf("expected floor emission, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"layer":"orchestrator_floor"`) {
		t.Errorf("expected layer=orchestrator_floor, got: %s", logOut)
	}
	// Same regression check as the allowlist-refusal path: the
	// model-supplied tool name MUST NOT appear in the floor's
	// attribution log; the `tool` attr is Sanitise'd first.
	if strings.Contains(logOut, leakyToolName) {
		t.Errorf("raw tool-name token surfaced in floor's own slog attribution: %s", logOut)
	}
	if !strings.Contains(logOut, `"tool":"AKIA…REDACTED"`) {
		t.Errorf("expected tool=AKIA…REDACTED (sanitised), got: %s", logOut)
	}

	content := extractToolResultText(t, mock)
	if strings.Contains(content, leakyToolName) {
		t.Errorf("raw tool-name token surfaced: %q", content)
	}
	if !strings.Contains(content, "AKIA…REDACTED") {
		t.Errorf("expected AKIA…REDACTED, got: %q", content)
	}
	secondCall := mock.calls[1]
	tr := secondCall.Messages[len(secondCall.Messages)-1].Content[0].OfToolResult
	if !tr.IsError.Value {
		t.Error("expected isError=true on unknown-tool result")
	}
}

// TestOrchestrator_FloorAppliedToDelegationSentinelPath pins that
// the "Delegating to ..." sentinel string (the third
// NewToolResultBlock site) also passes through
// sanitiseToolResultContent. The delegation path returns early from
// runToolLoop without making a second API call (the loopResult's
// turns hold the tool_result locally until the delegated crew runs),
// so the proof of the floor having executed is the slog emission —
// captured via redact.SetLogger.
//
// Setup: Maren's delegate_to_crew tool emits a sentinel pointing to
// a target crew named with a github_token shape. DelegateTool.Execute
// applies strings.ToLower to the crew name (line 55 of delegate.go)
// which rules out the case-sensitive AWS pattern; github_token is
// case-sensitive on the `ghp_` prefix but lowercase by default, so
// the post-lowercasing form still matches. The unknown target crew
// falls back to default routing (mirroring TestDelegationToUnknownCrewFallsToDefault).
func TestOrchestrator_FloorAppliedToDelegationSentinelPath(t *testing.T) {
	buf, restore := captureSlogForOrch(t)
	defer restore()

	// 44-char fixture: ghp_ + 40 lowercase chars. Survives ToLower,
	// matches the github_token pattern's {36,} minimum.
	leakyCrew := "ghp_" + strings.Repeat("a", 40)
	delegateInput := json.RawMessage(fmt.Sprintf(`{"crew":"%s","context":"go"}`, leakyCrew))

	// Maren delegates to the token-shape crew (unknown → falls back).
	toolReg := newToolRegistry(&echoTool{})
	o, _ := newTestOrchestrator(t, toolReg,
		toolUseWithTextResponse("delegating", "tu_1", "delegate_to_crew", delegateInput),
		textResponse("got it"), // fallback default-crew response
	)

	if _, err := o.Handle(t.Context(), "!room:server", "try delegate", "maren"); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	logOut := buf.String()
	if !strings.Contains(logOut, `"msg":"sanitiser_redaction"`) {
		t.Errorf("expected floor emission for delegation path, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"layer":"orchestrator_floor"`) {
		t.Errorf("expected layer=orchestrator_floor, got: %s", logOut)
	}
	// The block.Name for delegation is delegate_to_crew, NOT the target
	// crew. That's the orchestrator's invariant — the floor's tool
	// attribution identifies which tool produced the tool_result, not
	// which crew was being delegated to.
	if !strings.Contains(logOut, `"tool":"delegate_to_crew"`) {
		t.Errorf("expected tool=delegate_to_crew, got: %s", logOut)
	}
	// And the github_token pattern from the leaky crew name fired.
	if !strings.Contains(logOut, `"pattern_name":"github_token"`) {
		t.Errorf("expected pattern_name=github_token, got: %s", logOut)
	}
}

// TestOrchestrator_SanitiseFailClosedAtBoundary pins the end-to-end
// fail-closed contract demanded by #129 AC. Install a logger that
// panics on slog.LevelInfo emissions (the per-pattern attribution
// line); redact.SanitiseWith's deferred recover catches the panic
// and emits the Error-level line via the same logger (which does
// NOT panic on Error), then returns SanitiserErrorReplacement. The
// orchestrator floor passes that replacement straight through —
// the orchestrator never sees a raw input through a broken
// sanitiser, fulfilling the "fail-closed, never silently forward
// raw content" guarantee.
func TestOrchestrator_SanitiseFailClosedAtBoundary(t *testing.T) {
	restore := redact.SetLogger(slog.New(&panicOnInfoHandler{}))
	defer restore()

	// Input matches the AWS pattern so the floor's for-loop tries to
	// emit Info attribution, triggering the handler panic.
	in := "leaked AKIATESTKEY012345678 here"
	out := orchestrator.SanitiseToolResultContentForTest("any_tool", in)
	if out != redact.SanitiserErrorReplacement {
		t.Errorf("expected SanitiserErrorReplacement %q, got %q",
			redact.SanitiserErrorReplacement, out)
	}
	// Crucially, the raw token must NOT have surfaced in the
	// returned content (the fail-closed replacement substitutes the
	// entire string).
	if strings.Contains(out, "AKIATESTKEY012345678") {
		t.Errorf("raw token surfaced after fail-closed: %q", out)
	}
}

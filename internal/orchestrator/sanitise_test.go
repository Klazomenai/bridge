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

// captureSlogForOrch routes BOTH redact's "sanitiser_redaction"
// emissions (via redact.SetLogger) AND the orchestrator's own
// slog.Default emissions to a single JSON buffer. Tests inspect
// both surfaces:
//
//   - redact's floor emissions prove (or rule out) the floor fired.
//   - the orchestrator's own Warn/Info emissions are where
//     model-supplied identifiers (tool names, target crews) reach
//     log infrastructure. The path-1 / path-2 / path-3 tests assert
//     no raw token leaks via either surface.
//
// slog.SetDefault is process-global. Within a Go test binary the
// tests run sequentially (no t.Parallel() in this file), and the
// deferred restore unwinds the mutation; the slog.SetDefault hazard
// Copilot flagged in #170 round-8 was a cross-package concern that
// doesn't apply here since each go-test package binary is its own
// process. The redact side still uses redact.SetLogger, so the same
// principle holds: package-local, sequential, defer-restored.
func captureSlogForOrch(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	restoreRedact := redact.SetLogger(logger)
	originalDefault := slog.Default()
	slog.SetDefault(logger)
	return &buf, func() {
		slog.SetDefault(originalDefault)
		restoreRedact()
	}
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
// site in executeToolCalls) handles a token-shape block.Name
// without leaking it anywhere. Claude requests a tool whose Name is
// a token shape (AKIA-pattern); the orchestrator pre-sanitises
// block.Name once at the branch top and reuses the safe form for
// both the "tool not in crew allowlist" Warn log AND the
// tool_result content sent back to Claude. The floor's content scan
// then finds zero patterns (content already contains AKIA…REDACTED)
// and stays silent — assertions verify the raw name is absent from
// the orchestrator's own Warn log AND from the tool_result block.
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

	// (1) The orchestrator's OWN "tool not in crew allowlist" Warn
	//     line must NOT contain the raw model-supplied tool name.
	//     The orchestrator pre-sanitises block.Name once at the top
	//     of this branch and reuses the sanitised form for both the
	//     Warn log and the tool_result content; the raw token shape
	//     is therefore never given to a logging surface.
	logOut := buf.String()
	if strings.Contains(logOut, leakyToolName) {
		t.Errorf("raw tool-name token surfaced in slog output: %s", logOut)
	}
	if !strings.Contains(logOut, `"msg":"orchestrator: tool not in crew allowlist"`) {
		t.Errorf("expected orchestrator allowlist Warn line, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"tool":"AKIA…REDACTED"`) {
		t.Errorf("expected sanitised tool name in slog, got: %s", logOut)
	}

	// (2) The tool_result content sent back to Claude also carries
	//     the sanitised form (same pre-sanitisation, same name).
	content := extractToolResultText(t, mock)
	if strings.Contains(content, leakyToolName) {
		t.Errorf("raw tool-name token surfaced in tool_result: %q", content)
	}
	if !strings.Contains(content, "AKIA…REDACTED") {
		t.Errorf("expected AKIA…REDACTED in tool_result, got: %q", content)
	}
	// isError=true preserved (pre-sanitisation only touches the name
	// string; the error flag is untouched).
	secondCall := mock.calls[1]
	tr := secondCall.Messages[len(secondCall.Messages)-1].Content[0].OfToolResult
	if !tr.IsError.Value {
		t.Error("expected isError=true on allowlist-refusal result")
	}
}

// TestOrchestrator_FloorAppliedToUnknownToolPath pins that the
// "unknown tool" branch (the second NewToolResultBlock site) also
// handles a token-shape block.Name safely. Setup: the crew's
// allowlist DECLARES a token-shape tool name, but the registry
// doesn't have it — so the allowlist check passes and the registry
// lookup fails, hitting path 2. Same pre-sanitise pattern as path 1:
// safeName flows into both the Warn log AND the tool_result content,
// so the raw model-supplied name never reaches either surface. The
// floor's content scan sees AKIA…REDACTED (already safe) and stays
// silent; assertions check raw-name absence on both surfaces.
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

	// Same regression check as the allowlist-refusal path: the
	// model-supplied tool name MUST NOT appear in the orchestrator's
	// own "unknown tool requested" Warn log; block.Name is pre-
	// sanitised once and reused for both the Warn log and the
	// tool_result content.
	logOut := buf.String()
	if strings.Contains(logOut, leakyToolName) {
		t.Errorf("raw tool-name token surfaced in slog output: %s", logOut)
	}
	if !strings.Contains(logOut, `"msg":"orchestrator: unknown tool requested"`) {
		t.Errorf("expected orchestrator unknown-tool Warn line, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"tool":"AKIA…REDACTED"`) {
		t.Errorf("expected sanitised tool name in slog, got: %s", logOut)
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

	// The orchestrator's "delegation requested" slog.Info line
	// receives the pre-sanitised target crew name (the same
	// safeTarget is also used in the tool_result construction). The
	// raw model-supplied targetCrew must not appear in this line.
	//
	// Note: orchestrator.go's downstream "following delegation" and
	// "unknown crew member, falling back to default" lines still log
	// the raw targetCrew — those are outside #129's scope and
	// tracked as follow-up work. The buffer here may legitimately
	// contain the raw form on those lines; the assertion is
	// specifically scoped to the toolloop.go "delegation requested"
	// emission.
	logOut := buf.String()
	if !strings.Contains(logOut, `"msg":"orchestrator: delegation requested"`) {
		t.Errorf("expected orchestrator delegation Info line, got: %s", logOut)
	}
	// Scope: find the "delegation requested" line specifically and
	// check it. Using simple JSON-as-text containment with both
	// `msg` and `to` in the same line slice.
	for _, line := range strings.Split(logOut, "\n") {
		if !strings.Contains(line, `"msg":"orchestrator: delegation requested"`) {
			continue
		}
		if strings.Contains(line, leakyCrew) {
			t.Errorf("raw target-crew token surfaced in delegation-requested line: %s", line)
		}
		if !strings.Contains(line, `"to":"ghp_…REDACTED"`) {
			t.Errorf("expected to=ghp_…REDACTED (sanitised) in delegation-requested line, got: %s", line)
		}
		break
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

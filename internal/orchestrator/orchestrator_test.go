package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	ctxbuf "klazomenai/bridge/internal/context"
	"klazomenai/bridge/internal/crew"
	"klazomenai/bridge/internal/orchestrator"
)

const testCrewYAML = `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
  crest:
    name: "Crest"
    role: "Signalman"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      model: "en_US-lessac-high.onnx"
      announces_as: "Crest:"
    system_prompt: "You are Crest. Respond in {verbosity}"
`

// mockClaudeClient is a test double for orchestrator.ClaudeClient.
type mockClaudeClient struct {
	response *anthropic.Message
	err      error
	calls    []anthropic.MessageNewParams
}

func (m *mockClaudeClient) New(_ context.Context, body anthropic.MessageNewParams, _ ...option.RequestOption) (*anthropic.Message, error) {
	m.calls = append(m.calls, body)
	return m.response, m.err
}

func newMockResponse(text string) *anthropic.Message {
	return &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: text},
		},
	}
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

func newTestOrchestrator(t *testing.T) (*orchestrator.Orchestrator, *mockClaudeClient) {
	t.Helper()
	reg := newTestRegistry(t)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	mock := &mockClaudeClient{response: newMockResponse("Aye, that'll hold.")}
	return orchestrator.NewWithClient(reg, mgr, mock), mock
}

func TestNewCreatesOrchestrator(t *testing.T) {
	// New() creates a real Anthropic client (no network call — just struct init).
	reg := newTestRegistry(t)
	mgr := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)
	o := orchestrator.New(reg, mgr, "sk-test-key")
	if o == nil {
		t.Fatal("expected non-nil orchestrator")
	}
}

// --- Route tests ---

func TestRouteDefault(t *testing.T) {
	o, _ := newTestOrchestrator(t)
	c := o.Route("")
	if c.ID() != "maren" {
		t.Errorf("expected default crew maren, got %s", c.ID())
	}
}

func TestRouteNamedCrew(t *testing.T) {
	o, _ := newTestOrchestrator(t)
	c := o.Route("crest")
	if c.ID() != "crest" {
		t.Errorf("expected crest, got %s", c.ID())
	}
}

func TestRouteUnknownFallsToDefault(t *testing.T) {
	o, _ := newTestOrchestrator(t)
	c := o.Route("ghost")
	if c.ID() != "maren" {
		t.Errorf("expected fallback to maren, got %s", c.ID())
	}
}

func TestHandoffRequestRoutesCorrectly(t *testing.T) {
	o, _ := newTestOrchestrator(t)
	c := o.Route("crest")
	if c.ID() != "crest" {
		t.Errorf("expected crest on handoff, got %s", c.ID())
	}
}

// --- Handle tests ---

func TestHandleCallsClaude(t *testing.T) {
	o, mock := newTestOrchestrator(t)
	resp, err := o.Handle(t.Context(), "!room:server", "hull check", "")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if resp.Text != "Aye, that'll hold." {
		t.Errorf("unexpected response text: %q", resp.Text)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 API call, got %d", len(mock.calls))
	}
}

func TestHandleFramesPrefixAndCapsLength(t *testing.T) {
	o, mock := newTestOrchestrator(t)

	// Build a message over 1000 chars.
	long := strings.Repeat("x", 1500)
	_, err := o.Handle(t.Context(), "!room:server", long, "")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	call := mock.calls[0]
	// There should be exactly one message (user).
	if len(call.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(call.Messages))
	}
	body := call.Messages[0].Content[0].OfText.Text
	if !strings.HasPrefix(body, "The Captain says: ") {
		t.Errorf("expected captain prefix, got: %q", body[:40])
	}
	// Original 1500 chars capped at 1000 + prefix.
	if len(body) > len("The Captain says: ")+1000 {
		t.Errorf("message not capped: len=%d", len(body))
	}
}

func TestHandleContextBufferPassedToClaude(t *testing.T) {
	o, mock := newTestOrchestrator(t)

	// First turn.
	_, err := o.Handle(t.Context(), "!room:server", "hello", "")
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}
	// Second turn — history should be in messages.
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
	mock := &mockClaudeClient{err: errors.New("rate limited")}
	o := orchestrator.NewWithClient(reg, mgr, mock)

	_, err := o.Handle(t.Context(), "!room:server", "hello", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected rate limited in error, got: %v", err)
	}
}

func TestHandleRoutesToCrestWhenRequested(t *testing.T) {
	o, mock := newTestOrchestrator(t)
	resp, err := o.Handle(t.Context(), "!room:server", "check inbox", "crest")
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if resp.CrewID != "crest" {
		t.Errorf("expected crew crest, got %s", resp.CrewID)
	}
	// System prompt should be Crest's.
	call := mock.calls[0]
	if !strings.Contains(call.System[0].Text, "Crest") {
		t.Errorf("expected Crest system prompt, got: %q", call.System[0].Text[:50])
	}
}

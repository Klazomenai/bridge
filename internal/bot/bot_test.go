package bot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"klazomenai/bridge/internal/orchestrator"
)

// --- New() tests ---

func TestNewCreatesBot(t *testing.T) {
	orch := &mockOrch{}
	cfg := Config{
		Homeserver:  "https://matrix.example.com",
		Username:    "@bot:example.com",
		Password:    "secret",
		PickleKey:   "testpicklekey",
		DisplayName: "TestBot",
	}
	b, err := New(cfg, orch)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil bot")
	}
}

func TestNewDefaultsDisplayName(t *testing.T) {
	orch := &mockOrch{}
	cfg := Config{
		Homeserver: "https://matrix.example.com",
		Username:   "@bot:example.com",
		Password:   "s",
		PickleKey:  "k",
		// DisplayName intentionally omitted.
	}
	b, err := New(cfg, orch)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.cfg.DisplayName != "Bridge" {
		t.Errorf("expected default display name 'Bridge', got %q", b.cfg.DisplayName)
	}
}

func TestNewInvalidHomeserverReturnsError(t *testing.T) {
	orch := &mockOrch{}
	cfg := Config{
		Homeserver: "://bad-url",
		Username:   "@bot:example.com",
		Password:   "s",
		PickleKey:  "k",
	}
	_, err := New(cfg, orch)
	if err == nil {
		t.Fatal("expected error for bad homeserver URL")
	}
}

func TestNewDefaultsCryptoDBPath(t *testing.T) {
	orch := &mockOrch{}
	cfg := Config{
		Homeserver: "https://matrix.example.com",
		Username:   "@bot:example.com",
		Password:   "s",
		PickleKey:  "k",
		// CryptoDBPath intentionally omitted.
	}
	b, err := New(cfg, orch)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.cfg.CryptoDBPath == "" {
		t.Error("expected non-empty default CryptoDBPath")
	}
}

func TestMatrixSenderSendSuccess(t *testing.T) {
	// Stand up a fake Matrix homeserver that accepts the send-message PUT.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event_id":"$test123"}`))
	}))
	defer srv.Close()

	client, err := mautrix.NewClient(srv.URL, "@bot:server", "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	s := &matrixSender{client: client}
	resp := &orchestrator.Response{Text: "Aye", CrewID: "maren", Verbosity: "dispatch"}
	if err := s.Send(t.Context(), "!room:server", resp); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestMatrixSenderSendError(t *testing.T) {
	// Server returns a 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN","error":"internal error"}`))
	}))
	defer srv.Close()

	client, err := mautrix.NewClient(srv.URL, "@bot:server", "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	s := &matrixSender{client: client}
	resp := &orchestrator.Response{Text: "Aye", CrewID: "maren", Verbosity: "dispatch"}
	if err := s.Send(t.Context(), "!room:server", resp); err == nil {
		t.Fatal("expected error from 500 response")
	}
}

func TestRegisterHandlers(t *testing.T) {
	orch := &mockOrch{resp: &orchestrator.Response{Text: "Aye"}}
	sender := &mockSender{}
	b := newTestBot(t, orch, sender, "@bridge:server")
	// registerHandlers panics if the syncer isn't a DefaultSyncer; verify it doesn't.
	b.registerHandlers()
}

// --- extractCrewRequest tests (white-box, same package) ---

func TestExtractCrewRequestComma(t *testing.T) {
	got := extractCrewRequest("Crest, check the inbox please")
	if got != "crest" {
		t.Errorf("expected crest, got %q", got)
	}
}

func TestExtractCrewRequestColon(t *testing.T) {
	got := extractCrewRequest("Maren: what do you think about this hull design?")
	if got != "maren" {
		t.Errorf("expected maren, got %q", got)
	}
}

func TestExtractCrewRequestOverTo(t *testing.T) {
	got := extractCrewRequest("Maren, I'd like to hear from Crest — over to Crest on this one")
	if got != "crest" {
		t.Errorf("expected crest, got %q", got)
	}
}

func TestExtractCrewRequestNoMatch(t *testing.T) {
	got := extractCrewRequest("What's the weather like?")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractCrewRequestCaseInsensitive(t *testing.T) {
	got := extractCrewRequest("MAREN, strength check on this beam")
	if got != "maren" {
		t.Errorf("expected maren, got %q", got)
	}
}

func TestExtractCrewRequestOwnMessagesIgnored(t *testing.T) {
	got := extractCrewRequest("I've been speaking to Maren about this already")
	if got != "" {
		t.Errorf("expected no match, got %q", got)
	}
}

// --- handleMessage tests using mocks ---

// mockOrch is a test double for OrchestratorI.
type mockOrch struct {
	resp *orchestrator.Response
	err  error
	calls []string // roomID:text
}

func (m *mockOrch) Handle(_ context.Context, roomID, userText, _ string) (*orchestrator.Response, error) {
	m.calls = append(m.calls, roomID+":"+userText)
	return m.resp, m.err
}

// mockSender records Send calls.
type mockSender struct {
	calls []*orchestrator.Response
	err   error
}

func (s *mockSender) Send(_ context.Context, _ id.RoomID, resp *orchestrator.Response) error {
	s.calls = append(s.calls, resp)
	return s.err
}

// newTestBot creates a Bot with mock dependencies for unit testing.
// It does NOT call Start (no Matrix connection needed).
func newTestBot(t *testing.T, orch OrchestratorI, sender Sender, selfUserID id.UserID) *Bot {
	t.Helper()
	client, err := mautrix.NewClient("https://matrix.example.com", selfUserID, "test-token")
	if err != nil {
		t.Fatalf("mautrix.NewClient: %v", err)
	}
	return &Bot{
		client: client,
		orch:   orch,
		sender: sender,
		cfg:    Config{Username: string(selfUserID)},
	}
}

func textEvent(sender, roomID, body string) *event.Event {
	evt := &event.Event{
		Sender: id.UserID(sender),
		RoomID: id.RoomID(roomID),
		Type:   event.EventMessage,
	}
	evt.Content.Parsed = &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    body,
	}
	return evt
}

func TestIncomingMessageRouted(t *testing.T) {
	orch := &mockOrch{resp: &orchestrator.Response{Text: "Aye", CrewID: "maren", Verbosity: "dispatch"}}
	sender := &mockSender{}
	bot := newTestBot(t, orch, sender, "@bridge:server")

	bot.handleMessage(t.Context(), textEvent("@captain:server", "!room:server", "hull check"))

	if len(orch.calls) != 1 {
		t.Fatalf("expected 1 orchestrator call, got %d", len(orch.calls))
	}
	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 sender call, got %d", len(sender.calls))
	}
	if sender.calls[0].Text != "Aye" {
		t.Errorf("unexpected response text: %q", sender.calls[0].Text)
	}
}

func TestBotOwnMessagesIgnored(t *testing.T) {
	orch := &mockOrch{resp: &orchestrator.Response{Text: "irrelevant"}}
	sender := &mockSender{}
	bot := newTestBot(t, orch, sender, "@bridge:server")

	// Send a message from the bot's own user ID — should be ignored.
	bot.handleMessage(t.Context(), textEvent("@bridge:server", "!room:server", "self message"))

	if len(orch.calls) != 0 {
		t.Fatalf("expected 0 orchestrator calls for own message, got %d", len(orch.calls))
	}
}

func TestEmptyBodyIgnored(t *testing.T) {
	orch := &mockOrch{resp: &orchestrator.Response{Text: "irrelevant"}}
	sender := &mockSender{}
	bot := newTestBot(t, orch, sender, "@bridge:server")

	bot.handleMessage(t.Context(), textEvent("@captain:server", "!room:server", "   "))

	if len(orch.calls) != 0 {
		t.Fatalf("expected 0 orchestrator calls for empty message, got %d", len(orch.calls))
	}
}

func TestOrchestratorErrorDoesNotPanic(t *testing.T) {
	orch := &mockOrch{err: errors.New("api down")}
	sender := &mockSender{}
	bot := newTestBot(t, orch, sender, "@bridge:server")

	// Should not panic — error is logged.
	bot.handleMessage(t.Context(), textEvent("@captain:server", "!room:server", "hello"))

	if len(sender.calls) != 0 {
		t.Fatal("sender should not be called on orchestrator error")
	}
}

func TestNonTextMessageIgnored(t *testing.T) {
	orch := &mockOrch{resp: &orchestrator.Response{Text: "irrelevant"}}
	sender := &mockSender{}
	bot := newTestBot(t, orch, sender, "@bridge:server")

	// Send an image message — should be ignored.
	evt := &event.Event{
		Sender: id.UserID("@captain:server"),
		RoomID: id.RoomID("!room:server"),
		Type:   event.EventMessage,
	}
	evt.Content.Parsed = &event.MessageEventContent{
		MsgType: event.MsgImage,
		Body:    "image.jpg",
	}
	bot.handleMessage(t.Context(), evt)

	if len(orch.calls) != 0 {
		t.Fatalf("expected 0 orchestrator calls for image message, got %d", len(orch.calls))
	}
}

func TestCrewRequestExtractedBeforeRouting(t *testing.T) {
	orch := &mockOrch{resp: &orchestrator.Response{Text: "Signal received.", CrewID: "crest", Verbosity: "dispatch"}}
	sender := &mockSender{}
	bot := newTestBot(t, orch, sender, "@bridge:server")

	bot.handleMessage(t.Context(), textEvent("@captain:server", "!room:server", "Crest, check the inbox"))

	if len(orch.calls) != 1 {
		t.Fatal("expected orchestrator called")
	}
}

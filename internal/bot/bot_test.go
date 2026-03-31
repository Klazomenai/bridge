package bot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

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
	if b.cfg.CryptoDBPath != DefaultCryptoDBPath {
		t.Errorf("expected default CryptoDBPath %q, got %q", DefaultCryptoDBPath, b.cfg.CryptoDBPath)
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
	orch := &mockOrch{responses: []orchestrator.Response{{Text: "Aye"}}}
	sender := &mockSender{}
	b := newTestBot(t, orch, sender, "@bridge:server")
	// registerHandlers panics if the syncer isn't a DefaultSyncer; verify it doesn't.
	b.registerHandlers()
}

// --- extractCrewRequest tests (white-box, same package) ---

func TestExtractCrewRequestComma(t *testing.T) {
	got := extractCrewRequest("Crest, check the inbox please", []string{"maren", "crest"})
	if got != "crest" {
		t.Errorf("expected crest, got %q", got)
	}
}

func TestExtractCrewRequestColon(t *testing.T) {
	got := extractCrewRequest("Maren: what do you think about this hull design?", []string{"maren", "crest"})
	if got != "maren" {
		t.Errorf("expected maren, got %q", got)
	}
}

func TestExtractCrewRequestOverTo(t *testing.T) {
	got := extractCrewRequest("Maren, I'd like to hear from Crest — over to Crest on this one", []string{"maren", "crest"})
	if got != "crest" {
		t.Errorf("expected crest, got %q", got)
	}
}

func TestExtractCrewRequestNoMatch(t *testing.T) {
	got := extractCrewRequest("What's the weather like?", []string{"maren", "crest"})
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractCrewRequestCaseInsensitive(t *testing.T) {
	got := extractCrewRequest("MAREN, strength check on this beam", []string{"maren", "crest"})
	if got != "maren" {
		t.Errorf("expected maren, got %q", got)
	}
}

func TestExtractCrewRequestOwnMessagesIgnored(t *testing.T) {
	got := extractCrewRequest("I've been speaking to Maren about this already", []string{"maren", "crest"})
	if got != "" {
		t.Errorf("expected no match, got %q", got)
	}
}

func TestExtractCrewRequestWordBoundary(t *testing.T) {
	// "crest" must not match "crestfallen" — word boundary required after crew ID.
	got := extractCrewRequest("over to crestfallen horizon", []string{"crest"})
	if got != "" {
		t.Errorf("expected no match for partial word, got %q", got)
	}
	// Exact match at end-of-string must still work.
	got = extractCrewRequest("over to crest", []string{"crest"})
	if got != "crest" {
		t.Errorf("expected crest, got %q", got)
	}
	// Followed by punctuation/space is fine.
	got = extractCrewRequest("over to crest, please check the inbox", []string{"crest"})
	if got != "crest" {
		t.Errorf("expected crest, got %q", got)
	}
}

func TestExtractCrewRequestBosun(t *testing.T) {
	crew := []string{"maren", "crest", "bosun", "lookout"}
	got := extractCrewRequest("over to bosun", crew)
	if got != "bosun" {
		t.Errorf("expected bosun, got %q", got)
	}
}

func TestExtractCrewRequestLookoutComma(t *testing.T) {
	crew := []string{"maren", "crest", "bosun", "lookout"}
	got := extractCrewRequest("lookout, status report", crew)
	if got != "lookout" {
		t.Errorf("expected lookout, got %q", got)
	}
}

func TestExtractCrewRequestLookoutColon(t *testing.T) {
	crew := []string{"maren", "crest", "bosun", "lookout"}
	got := extractCrewRequest("lookout: check the metrics", crew)
	if got != "lookout" {
		t.Errorf("expected lookout, got %q", got)
	}
}

func TestExtractCrewRequestMultipleOverToPicksEarliest(t *testing.T) {
	crew := []string{"maren", "crest", "bosun", "lookout", "chips"}

	msgCrestFirst := "over to crest for comms, then over to chips for the PR"
	got := extractCrewRequest(msgCrestFirst, crew)
	if got != "crest" {
		t.Errorf("expected crest (earliest match), got %q", got)
	}

	// Reverse scenario: chips appears first.
	msgChipsFirst := "over to chips first, then over to bosun"
	got = extractCrewRequest(msgChipsFirst, crew)
	if got != "chips" {
		t.Errorf("expected chips (earliest match), got %q", got)
	}

	// Verify that changing the known crew slice order does not affect routing.
	// Reverse the crew slice to simulate non-deterministic map iteration order.
	reversed := make([]string, len(crew))
	for i := range crew {
		reversed[i] = crew[len(crew)-1-i]
	}

	got = extractCrewRequest(msgCrestFirst, reversed)
	if got != "crest" {
		t.Errorf("with reversed crew slice, expected crest (earliest match), got %q", got)
	}

	got = extractCrewRequest(msgChipsFirst, reversed)
	if got != "chips" {
		t.Errorf("with reversed crew slice, expected chips (earliest match), got %q", got)
	}
}

func TestExtractCrewRequestChipsOverTo(t *testing.T) {
	crew := []string{"maren", "crest", "bosun", "lookout", "chips"}
	got := extractCrewRequest("over to chips", crew)
	if got != "chips" {
		t.Errorf("expected chips, got %q", got)
	}
}

func TestExtractCrewRequestChipsComma(t *testing.T) {
	crew := []string{"maren", "crest", "bosun", "lookout", "chips"}
	got := extractCrewRequest("chips, check the PR", crew)
	if got != "chips" {
		t.Errorf("expected chips, got %q", got)
	}
}

// --- handleMessage tests using mocks ---

// mockOrch is a test double for OrchestratorI.
type mockOrch struct {
	responses    []orchestrator.Response
	err          error
	calls        []string // roomID:text
	crewRequests []string // requestedCrew arg per call
}

func (m *mockOrch) Handle(_ context.Context, roomID, userText, requestedCrew string) ([]orchestrator.Response, error) {
	m.calls = append(m.calls, roomID+":"+userText)
	m.crewRequests = append(m.crewRequests, requestedCrew)
	return m.responses, m.err
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

// mockTyper records typing calls.
type mockTyper struct {
	calls []mockTypingCall
}

type mockTypingCall struct {
	typing bool
}

func (t *mockTyper) SetTyping(_ context.Context, _ id.RoomID, typing bool, _ time.Duration) error {
	t.calls = append(t.calls, mockTypingCall{typing: typing})
	return nil
}

// newTestBot creates a Bot with mock dependencies for unit testing.
// It does NOT call Start (no Matrix connection needed).
func newTestBot(t *testing.T, orch OrchestratorI, sender Sender, selfUserID id.UserID) *Bot {
	t.Helper()
	return newTestBotWithTyper(t, orch, sender, &mockTyper{}, selfUserID)
}

func newTestBotWithTyper(t *testing.T, orch OrchestratorI, sender Sender, typer Typer, selfUserID id.UserID) *Bot {
	t.Helper()
	client, err := mautrix.NewClient("https://matrix.example.com", selfUserID, "test-token")
	if err != nil {
		t.Fatalf("mautrix.NewClient: %v", err)
	}
	return &Bot{
		client: client,
		orch:   orch,
		sender: sender,
		typer:  typer,
		cfg: Config{
			Username:  string(selfUserID),
			KnownCrew: []string{"maren", "crest", "bosun", "lookout", "chips"},
			RoomAllowlist: map[id.RoomID]struct{}{
				"!room:server": {},
			},
		},
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
	orch := &mockOrch{responses: []orchestrator.Response{{Text: "Aye", CrewID: "maren", Verbosity: "dispatch"}}}
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
	orch := &mockOrch{responses: []orchestrator.Response{{Text: "irrelevant"}}}
	sender := &mockSender{}
	bot := newTestBot(t, orch, sender, "@bridge:server")

	// Send a message from the bot's own user ID — should be ignored.
	bot.handleMessage(t.Context(), textEvent("@bridge:server", "!room:server", "self message"))

	if len(orch.calls) != 0 {
		t.Fatalf("expected 0 orchestrator calls for own message, got %d", len(orch.calls))
	}
}

func TestEmptyBodyIgnored(t *testing.T) {
	orch := &mockOrch{responses: []orchestrator.Response{{Text: "irrelevant"}}}
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
	orch := &mockOrch{responses: []orchestrator.Response{{Text: "irrelevant"}}}
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
	orch := &mockOrch{responses: []orchestrator.Response{{Text: "Signal received.", CrewID: "crest", Verbosity: "dispatch"}}}
	sender := &mockSender{}
	bot := newTestBot(t, orch, sender, "@bridge:server")

	bot.handleMessage(t.Context(), textEvent("@captain:server", "!room:server", "Crest, check the inbox"))

	if len(orch.calls) != 1 {
		t.Fatal("expected orchestrator called once")
	}
	// Verify crew extraction: "Crest," prefix must result in requestedCrew="crest".
	if orch.crewRequests[0] != "crest" {
		t.Errorf("expected requestedCrew=%q, got %q", "crest", orch.crewRequests[0])
	}
}

func TestMultipleResponsesSentSeparately(t *testing.T) {
	orch := &mockOrch{responses: []orchestrator.Response{
		{Text: "Hull's sound.", CrewID: "maren", Verbosity: "dispatch"},
		{Text: "Signal received.", CrewID: "crest", Verbosity: "dispatch"},
	}}
	sender := &mockSender{}
	bot := newTestBot(t, orch, sender, "@bridge:server")

	bot.handleMessage(t.Context(), textEvent("@captain:server", "!room:server", "status report"))

	if len(sender.calls) != 2 {
		t.Fatalf("expected 2 sender calls, got %d", len(sender.calls))
	}
	if sender.calls[0].CrewID != "maren" {
		t.Errorf("first send crew = %q, want maren", sender.calls[0].CrewID)
	}
	if sender.calls[1].CrewID != "crest" {
		t.Errorf("second send crew = %q, want crest", sender.calls[1].CrewID)
	}
}

// eventLog records the order of typing and send operations for ordering assertions.
type eventLog struct {
	events []string
}

// orderingSender wraps mockSender and logs to a shared eventLog.
type orderingSender struct {
	inner *mockSender
	log   *eventLog
}

func (s *orderingSender) Send(ctx context.Context, roomID id.RoomID, resp *orchestrator.Response) error {
	s.log.events = append(s.log.events, "send:"+resp.CrewID)
	return s.inner.Send(ctx, roomID, resp)
}

// orderingTyper wraps mockTyper and logs to a shared eventLog.
type orderingTyper struct {
	inner *mockTyper
	log   *eventLog
}

func (t *orderingTyper) SetTyping(ctx context.Context, roomID id.RoomID, typing bool, timeout time.Duration) error {
	if typing {
		t.log.events = append(t.log.events, "typing:start")
	} else {
		t.log.events = append(t.log.events, "typing:stop")
	}
	return t.inner.SetTyping(ctx, roomID, typing, timeout)
}

func TestTypingIndicatorSentBeforeResponse(t *testing.T) {
	orch := &mockOrch{responses: []orchestrator.Response{{Text: "Aye", CrewID: "maren", Verbosity: "dispatch"}}}
	log := &eventLog{}
	sender := &orderingSender{inner: &mockSender{}, log: log}
	typer := &orderingTyper{inner: &mockTyper{}, log: log}
	bot := newTestBotWithTyper(t, orch, sender, typer, "@bridge:server")

	bot.handleMessage(t.Context(), textEvent("@captain:server", "!room:server", "hull check"))

	// Verify ordering: typing:start → typing:stop → send:maren
	if len(log.events) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(log.events), log.events)
	}
	if log.events[0] != "typing:start" {
		t.Errorf("event[0] = %q, want typing:start", log.events[0])
	}
	if log.events[1] != "typing:stop" {
		t.Errorf("event[1] = %q, want typing:stop", log.events[1])
	}
	if log.events[2] != "send:maren" {
		t.Errorf("event[2] = %q, want send:maren", log.events[2])
	}
}

// slowOrch simulates a slow orchestrator that respects context cancellation.
type slowOrch struct {
	delay     time.Duration
	responses []orchestrator.Response
	err       error
}

func (m *slowOrch) Handle(ctx context.Context, _, _, _ string) ([]orchestrator.Response, error) {
	select {
	case <-time.After(m.delay):
		return m.responses, m.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// --- Room allowlist tests ---

func TestIsRoomAllowedPopulated(t *testing.T) {
	b := newTestBot(t, &mockOrch{}, &mockSender{}, "@bridge:server")
	b.cfg.RoomAllowlist = map[id.RoomID]struct{}{
		"!allowed:server": {},
	}

	if !b.isRoomAllowed("!allowed:server") {
		t.Error("expected allowed room to be permitted")
	}
	if b.isRoomAllowed("!other:server") {
		t.Error("expected disallowed room to be rejected")
	}
}

func TestIsRoomAllowedEmpty(t *testing.T) {
	b := newTestBot(t, &mockOrch{}, &mockSender{}, "@bridge:server")
	b.cfg.RoomAllowlist = map[id.RoomID]struct{}{}

	if b.isRoomAllowed("!any:server") {
		t.Error("empty allowlist should deny all rooms (fail-closed)")
	}
}

func TestIsRoomAllowedNil(t *testing.T) {
	b := newTestBot(t, &mockOrch{}, &mockSender{}, "@bridge:server")
	b.cfg.RoomAllowlist = nil

	if b.isRoomAllowed("!any:server") {
		t.Error("nil allowlist should deny all rooms (fail-closed)")
	}
}

func TestMessageIgnoredForDisallowedRoom(t *testing.T) {
	orch := &mockOrch{responses: []orchestrator.Response{{Text: "Aye"}}}
	sender := &mockSender{}
	b := newTestBot(t, orch, sender, "@bridge:server")
	b.cfg.RoomAllowlist = map[id.RoomID]struct{}{
		"!allowed:server": {},
	}

	b.handleMessage(t.Context(), textEvent("@captain:server", "!disallowed:server", "hull check"))

	if len(orch.calls) != 0 {
		t.Fatalf("expected 0 orchestrator calls for disallowed room, got %d", len(orch.calls))
	}
}

func TestMessageAllowedRoom(t *testing.T) {
	orch := &mockOrch{responses: []orchestrator.Response{{Text: "Aye", CrewID: "maren", Verbosity: "dispatch"}}}
	sender := &mockSender{}
	b := newTestBot(t, orch, sender, "@bridge:server")
	b.cfg.RoomAllowlist = map[id.RoomID]struct{}{
		"!allowed:server": {},
	}

	b.handleMessage(t.Context(), textEvent("@captain:server", "!allowed:server", "hull check"))

	if len(orch.calls) != 1 {
		t.Fatalf("expected 1 orchestrator call for allowed room, got %d", len(orch.calls))
	}
}

func TestEnforceRoomAllowlist(t *testing.T) {
	var leftRooms []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/joined_rooms"):
			_, _ = w.Write([]byte(`{"joined_rooms":["!allowed:server","!disallowed:server"]}`))
		case strings.HasSuffix(r.URL.Path, "/leave"):
			// Extract room ID from /_matrix/client/v3/rooms/{roomID}/leave
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 2 {
				decoded, _ := url.PathUnescape(parts[len(parts)-2])
				leftRooms = append(leftRooms, decoded)
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client, err := mautrix.NewClient(srv.URL, "@bridge:server", "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	b := &Bot{
		client: client,
		cfg: Config{
			RoomAllowlist: map[id.RoomID]struct{}{
				"!allowed:server": {},
			},
		},
	}

	b.enforceRoomAllowlist(t.Context())

	if len(leftRooms) != 1 {
		t.Fatalf("expected 1 leave call, got %d", len(leftRooms))
	}
	if leftRooms[0] != "!disallowed:server" {
		t.Errorf("expected to leave !disallowed:server, left %q", leftRooms[0])
	}
}

func TestInviteRejectedForDisallowedRoom(t *testing.T) {
	var leftRooms []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/leave") {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 2 {
				decoded, _ := url.PathUnescape(parts[len(parts)-2])
				leftRooms = append(leftRooms, decoded)
			}
			_, _ = w.Write([]byte(`{}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client, err := mautrix.NewClient(srv.URL, "@bridge:server", "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	b := &Bot{
		client: client,
		cfg: Config{
			RoomAllowlist: map[id.RoomID]struct{}{
				"!allowed:server": {},
			},
		},
	}
	b.registerHandlers()

	// Simulate an invite event for a disallowed room.
	evt := &event.Event{
		Sender: id.UserID("@attacker:server"),
		RoomID: id.RoomID("!evil:server"),
		Type:   event.StateMember,
	}
	evt.Content.Parsed = &event.MemberEventContent{
		Membership: event.MembershipInvite,
	}
	stateKey := "@bridge:server"
	evt.StateKey = &stateKey

	syncer := client.Syncer.(*mautrix.DefaultSyncer)
	_ = syncer.ProcessResponse(t.Context(), &mautrix.RespSync{
		Rooms: mautrix.RespSyncRooms{
			Invite: map[id.RoomID]*mautrix.SyncInvitedRoom{
				"!evil:server": {
					State: mautrix.SyncEventsList{
						Events: []*event.Event{evt},
					},
				},
			},
		},
	}, "")

	if len(leftRooms) != 1 {
		t.Fatalf("expected 1 leave call (reject invite), got %d", len(leftRooms))
	}
	if leftRooms[0] != "!evil:server" {
		t.Errorf("expected to reject !evil:server, rejected %q", leftRooms[0])
	}
}

func TestInviteAcceptedForAllowedRoom(t *testing.T) {
	var joinedRooms []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/join") {
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 2 {
				decoded, _ := url.PathUnescape(parts[len(parts)-2])
				joinedRooms = append(joinedRooms, decoded)
			}
			_, _ = w.Write([]byte(`{"room_id":"` + joinedRooms[len(joinedRooms)-1] + `"}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client, err := mautrix.NewClient(srv.URL, "@bridge:server", "test-token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	b := &Bot{
		client: client,
		cfg: Config{
			RoomAllowlist: map[id.RoomID]struct{}{
				"!welcome:server": {},
			},
		},
	}
	b.registerHandlers()

	evt := &event.Event{
		Sender: id.UserID("@captain:server"),
		RoomID: id.RoomID("!welcome:server"),
		Type:   event.StateMember,
	}
	evt.Content.Parsed = &event.MemberEventContent{
		Membership: event.MembershipInvite,
	}
	stateKey := "@bridge:server"
	evt.StateKey = &stateKey

	syncer := client.Syncer.(*mautrix.DefaultSyncer)
	_ = syncer.ProcessResponse(t.Context(), &mautrix.RespSync{
		Rooms: mautrix.RespSyncRooms{
			Invite: map[id.RoomID]*mautrix.SyncInvitedRoom{
				"!welcome:server": {
					State: mautrix.SyncEventsList{
						Events: []*event.Event{evt},
					},
				},
			},
		},
	}, "")

	if len(joinedRooms) != 1 {
		t.Fatalf("expected 1 join call, got %d", len(joinedRooms))
	}
	if joinedRooms[0] != "!welcome:server" {
		t.Errorf("expected to join !welcome:server, joined %q", joinedRooms[0])
	}
}

func TestTimeoutSendsGracefulMessage(t *testing.T) {
	// Use a very short timeout by calling awaitWithTyping directly.
	orch := &slowOrch{delay: 5 * time.Second, responses: []orchestrator.Response{{Text: "too late"}}}
	sender := &mockSender{}
	typer := &mockTyper{}
	bot := newTestBotWithTyper(t, orch, sender, typer, "@bridge:server")

	// Create a context that times out quickly.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	ch := make(chan orchResult, 1)
	go func() {
		responses, err := orch.Handle(ctx, "!room:server", "test", "")
		ch <- orchResult{responses, err}
	}()

	bot.awaitWithTyping(t.Context(), ctx, "!room:server", ch)

	// Should have sent the timeout message.
	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 sender call (timeout message), got %d", len(sender.calls))
	}
	if sender.calls[0].Text != "The crew ran out of time on this one, Captain." {
		t.Errorf("unexpected timeout text: %q", sender.calls[0].Text)
	}
	// Typing should have been cancelled.
	last := typer.calls[len(typer.calls)-1]
	if last.typing {
		t.Error("typing should be cancelled after timeout")
	}
}

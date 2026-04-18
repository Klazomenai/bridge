package context_test

import (
	"fmt"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	ctxbuf "klazomenai/bridge/internal/context"
)

// --- message builders ---

func userMsg(text string) anthropic.MessageParam {
	return anthropic.NewUserMessage(anthropic.NewTextBlock(text))
}

func assistantMsg(text string) anthropic.MessageParam {
	return anthropic.NewAssistantMessage(anthropic.NewTextBlock(text))
}

// toolUseMsg builds an assistant message carrying a single tool_use block.
// Mirrors how the orchestrator constructs intermediate turns in
// `internal/orchestrator/toolloop.go:88`.
func toolUseMsg(id, name string) anthropic.MessageParam {
	return anthropic.NewAssistantMessage(anthropic.NewToolUseBlock(id, nil, name))
}

// toolResultMsg builds a user message carrying a single tool_result block.
// Mirrors `internal/orchestrator/toolloop.go:89` using
// `anthropic.NewToolResultBlock(id, content, isError=false)`.
func toolResultMsg(id, content string) anthropic.MessageParam {
	return anthropic.NewUserMessage(anthropic.NewToolResultBlock(id, content, false))
}

// toolResultErrorMsg is the stub-tool shape: an error-flagged tool_result.
// Produced by `internal/tools/stub.go:30` → `NewToolResultBlock(id, msg, true)`.
func toolResultErrorMsg(id, content string) anthropic.MessageParam {
	return anthropic.NewUserMessage(anthropic.NewToolResultBlock(id, content, true))
}

// --- invariant verifier ---

// assertValidSequence walks msgs and asserts the Anthropic API messages-array
// invariant: every tool_result block in any message has the matching tool_use
// block (by ID) in the immediately preceding assistant message. Additionally
// asserts the buffer either starts with a user message whose content carries
// no tool_result, or is empty.
//
// If the invariant fails, the test reports the offending sequence so failures
// from `TestAdd_PropertyBufferAlwaysValid` are reproducible.
func assertValidSequence(t *testing.T, msgs []anthropic.MessageParam) {
	t.Helper()
	if len(msgs) == 0 {
		return
	}

	// First message must be a user message with no tool_result.
	if msgs[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("invariant: buffer must start with a user message, got role %q", msgs[0].Role)
		return
	}
	for j := range msgs[0].Content {
		if msgs[0].Content[j].OfToolResult != nil {
			t.Errorf("invariant: buffer starts with an orphaned tool_result (no preceding tool_use)")
			return
		}
	}

	// Every tool_result must have a preceding tool_use in the prior message
	// with a matching ID.
	for i := 0; i < len(msgs); i++ {
		for j := range msgs[i].Content {
			tr := msgs[i].Content[j].OfToolResult
			if tr == nil {
				continue
			}
			if i == 0 {
				t.Errorf("invariant: tool_result at index 0 has no preceding message (ID=%q)", tr.ToolUseID)
				return
			}
			prev := msgs[i-1]
			found := false
			for k := range prev.Content {
				tu := prev.Content[k].OfToolUse
				if tu != nil && tu.ID == tr.ToolUseID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("invariant: tool_result ID=%q at msg[%d] has no matching tool_use in msg[%d]",
					tr.ToolUseID, i, i-1)
				return
			}
		}
	}
}

// --- original tests, unchanged ---

func TestNewConversationBufferDefaultMaxTurns(t *testing.T) {
	// maxTurns <= 0 should fall back to DefaultMaxTurns.
	buf := ctxbuf.NewConversationBuffer(0)
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}
	// Add more than DefaultMaxTurns*2 messages to prove the default cap is applied.
	for i := 0; i < ctxbuf.DefaultMaxTurns+2; i++ {
		buf.Add(userMsg("u"))
		buf.Add(assistantMsg("a"))
	}
	msgs := buf.Messages()
	if len(msgs) > ctxbuf.DefaultMaxTurns*2 {
		t.Errorf("buffer exceeded default max: got %d, max %d", len(msgs), ctxbuf.DefaultMaxTurns*2)
	}
}

func TestBufferStoresTurns(t *testing.T) {
	buf := ctxbuf.NewConversationBuffer(10)
	buf.Add(userMsg("hello"))
	buf.Add(assistantMsg("ahoy"))

	msgs := buf.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestBufferRespectsMaxTurns(t *testing.T) {
	buf := ctxbuf.NewConversationBuffer(2) // 2 turns = 4 messages max

	// Add 3 turns (6 messages) — oldest pair should be evicted.
	buf.Add(userMsg("msg1"))
	buf.Add(assistantMsg("rsp1"))
	buf.Add(userMsg("msg2"))
	buf.Add(assistantMsg("rsp2"))
	buf.Add(userMsg("msg3"))
	buf.Add(assistantMsg("rsp3"))

	msgs := buf.Messages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (2 turns), got %d", len(msgs))
	}
	// First message in buffer should be msg2, not msg1. The block-aware
	// algorithm produces the same result as slice-by-2 for pure-text input
	// because every even index is a user(text) message, so the first safe
	// boundary at or after minEvict=2 is msg2.
	block := msgs[0].Content[0].OfText
	if block == nil || block.Text != "msg2" {
		t.Errorf("expected oldest retained message to be msg2, got %+v", msgs[0])
	}
}

func TestBufferClear(t *testing.T) {
	buf := ctxbuf.NewConversationBuffer(10)
	buf.Add(userMsg("x"))
	buf.Clear()
	if len(buf.Messages()) != 0 {
		t.Fatal("expected empty buffer after Clear")
	}
}

func TestBufferKeyedPerRoom(t *testing.T) {
	mgr := ctxbuf.NewManager(10)

	buf1 := mgr.Buffer("room1")
	buf2 := mgr.Buffer("room2")

	buf1.Add(userMsg("r1-msg"))
	buf2.Add(userMsg("r2-msg"))

	if len(buf1.Messages()) != 1 || len(buf2.Messages()) != 1 {
		t.Fatal("room buffers should be independent")
	}

	// Same room ID should return the same buffer.
	if mgr.Buffer("room1") != buf1 {
		t.Fatal("expected same buffer instance for room1")
	}
}

func TestManagerReturnsSameBuffer(t *testing.T) {
	mgr := ctxbuf.NewManager(10)
	a := mgr.Buffer("room-x")
	b := mgr.Buffer("room-x")
	if a != b {
		t.Fatal("expected identical buffer for same room")
	}
}

// --- new tests for block-aware eviction (issue #99) ---

// addToolTurn appends one Captain turn that exercises numTools tool-use
// round-trips plus a final text response. Shape:
//
//	user(text) → (assistant(tool_use) + user(tool_result)) * numTools → assistant(text)
//
// Total entries added = 2 + 2*numTools.
func addToolTurn(buf *ctxbuf.ConversationBuffer, turnID string, numTools int) {
	buf.Add(userMsg(turnID + "-q"))
	for t := 0; t < numTools; t++ {
		id := fmt.Sprintf("%s-tu-%d", turnID, t)
		buf.Add(toolUseMsg(id, "kubectl_get"))
		buf.Add(toolResultMsg(id, "result"))
	}
	buf.Add(assistantMsg(turnID + "-a"))
}

// TestAdd_EvictsWholeToolRoundTrip constructs a buffer whose round-trip
// alignment makes slice-by-2 eviction split a tool_use/tool_result pair.
// Verifies the new block-aware algorithm evicts to a safe boundary instead.
func TestAdd_EvictsWholeToolRoundTrip(t *testing.T) {
	buf := ctxbuf.NewConversationBuffer(10) // maxEntries = 20

	// Round 1: 1 tool (4 entries)      — first Captain input at index 0.
	addToolTurn(buf, "r1", 1)
	// Round 2: 2 tools (6 entries)     — second Captain input at index 4.
	addToolTurn(buf, "r2", 2)
	// Rounds 3..5: 1 tool each (12 entries).
	addToolTurn(buf, "r3", 1)
	addToolTurn(buf, "r4", 1)
	addToolTurn(buf, "r5", 1)
	// Buffer is now at 22 entries — over the 20-cap by 2. Slice-by-2 would
	// have evicted msgs[0..1], leaving an orphaned tool_result at index 0.
	// The block-aware algorithm walks to the next safe boundary (the r2
	// Captain input at the ORIGINAL index 4).
	msgs := buf.Messages()
	assertValidSequence(t, msgs)

	// Explicit shape assertion: buffer must now start with r2's user
	// message (the first safe boundary at or after minEvict=2).
	if len(msgs) == 0 {
		t.Fatal("expected non-empty buffer after eviction")
	}
	text := msgs[0].Content[0].OfText
	if text == nil || text.Text != "r2-q" {
		t.Errorf("expected buffer to start with r2-q, got %+v", msgs[0])
	}
}

// TestAdd_StubToolResultErrorStillEvictsAsPair verifies error-flagged
// tool_result blocks (is_error=true, produced by `internal/tools/stub.go:30`)
// are treated identically to success tool_result blocks for the purpose of
// the invariant.
func TestAdd_StubToolResultErrorStillEvictsAsPair(t *testing.T) {
	buf := ctxbuf.NewConversationBuffer(4) // maxEntries = 8

	// Captain asks something, Claude calls a stub tool, stub returns error,
	// Claude composes a text response. Repeat.
	buf.Add(userMsg("q1"))
	buf.Add(toolUseMsg("tu1", "imap_poll"))
	buf.Add(toolResultErrorMsg("tu1", "tool not available"))
	buf.Add(assistantMsg("sorry, email is offline"))
	buf.Add(userMsg("q2"))
	buf.Add(toolUseMsg("tu2", "imap_poll"))
	buf.Add(toolResultErrorMsg("tu2", "tool not available"))
	buf.Add(assistantMsg("still offline"))
	// 8 entries exactly — no eviction yet.
	buf.Add(userMsg("q3"))
	// 9 entries — eviction should fire and land on a safe boundary.
	assertValidSequence(t, buf.Messages())
}

// TestAdd_EvictionRespectsLongestRoundTrip simulates the pathological case
// where one Captain turn runs the full maxToolIterations=5 rounds. The
// resulting 12-entry turn is then followed by a short turn that trips
// eviction. The evictor must find the boundary between turns, not mid-turn.
func TestAdd_EvictionRespectsLongestRoundTrip(t *testing.T) {
	buf := ctxbuf.NewConversationBuffer(8) // maxEntries = 16

	// Round 1: 5 tools (12 entries).
	addToolTurn(buf, "big", 5)
	// Round 2: 2 tools (6 entries). Total 18 entries, over the 16-cap.
	addToolTurn(buf, "small", 2)

	msgs := buf.Messages()
	assertValidSequence(t, msgs)

	// The boundary is the "small-q" user message at the original index 12.
	// Everything before it (the 12-entry big round) is evicted.
	if len(msgs) == 0 {
		t.Fatal("expected non-empty buffer")
	}
	text := msgs[0].Content[0].OfText
	if text == nil || text.Text != "small-q" {
		t.Errorf("expected buffer to start with small-q, got %+v", msgs[0])
	}
}

// TestAdd_UnboundedToolLoopClears constructs a buffer where the only user
// message is ancient (below minEvict) and every subsequent message is part
// of a tool round-trip. When eviction finds no safe boundary, it clears the
// buffer rather than produce a malformed sequence.
func TestAdd_UnboundedToolLoopClears(t *testing.T) {
	buf := ctxbuf.NewConversationBuffer(2) // maxEntries = 4

	// Single Captain input starts the buffer.
	buf.Add(userMsg("initial"))
	// Then an unbounded stream of tool round-trips with no fresh Captain
	// input. In real life runToolLoop caps at maxToolIterations=5 so this
	// is contrived, but we test the fallback anyway.
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("tu-%d", i)
		buf.Add(toolUseMsg(id, "kubectl_get"))
		buf.Add(toolResultMsg(id, "result"))
	}

	// The buffer should have been cleared because the only safe boundary
	// (the "initial" user message at index 0) was below minEvict by the
	// time eviction fired.
	msgs := buf.Messages()
	if len(msgs) != 0 {
		t.Errorf("expected empty buffer after fallback Clear, got %d entries", len(msgs))
	}
}

// TestAdd_PureTextStillWorks proves the block-aware algorithm produces the
// same result as slice-by-2 for pure-text conversations. Regression guard
// for any future refactor that might claim the old behaviour was needed.
func TestAdd_PureTextStillWorks(t *testing.T) {
	buf := ctxbuf.NewConversationBuffer(5) // maxEntries = 10

	for i := 0; i < 30; i++ {
		buf.Add(userMsg(fmt.Sprintf("u%d", i)))
		buf.Add(assistantMsg(fmt.Sprintf("a%d", i)))
	}

	msgs := buf.Messages()
	if len(msgs) != 10 {
		t.Errorf("expected buffer length 10, got %d", len(msgs))
	}
	// First retained message must be user 25 (u25/a25 .. u29/a29 = 10 entries).
	text := msgs[0].Content[0].OfText
	if text == nil || text.Text != "u25" {
		t.Errorf("expected buffer to start with u25, got %+v", msgs[0])
	}
	assertValidSequence(t, msgs)
}

// TestAdd_SoftMaxTurnsOvershootClearsBuffer verifies that when a single
// tool-use turn exceeds maxEntries and the only safe boundary (the initial
// user-text at index 0) is below minEvict, the evictor falls back to
// Clear() rather than splitting the turn. The user notices context loss
// ("Maren forgot what we were talking about") but the invariant holds.
func TestAdd_SoftMaxTurnsOvershootClearsBuffer(t *testing.T) {
	buf := ctxbuf.NewConversationBuffer(3) // maxEntries = 6

	// One big round-trip: 1 user + 3 tool rounds + 1 final = 8 entries.
	// This exceeds maxEntries. The only user-text boundary is at index 0,
	// which is below minEvict=2 by the time eviction fires. No safe
	// boundary exists at or after minEvict → Clear().
	addToolTurn(buf, "once", 3)

	msgs := buf.Messages()
	assertValidSequence(t, msgs)
	// Buffer is empty: the fallback fired because the only boundary was
	// before minEvict. The post-condition guard then clears any residual
	// non-user-text messages left by the tool round.
	if len(msgs) != 0 {
		t.Errorf("expected empty buffer (Clear fallback), got %d entries", len(msgs))
	}
}

// TestAdd_PropertyBufferAlwaysValid is the invariant guard. For each
// pre-defined Add sequence (a mix of text turns and tool turns of varying
// sizes), it replays the sequence against a buffer and asserts after EVERY
// single Add that the buffer is a valid messages array. A future regression
// that slices through a tool-use pair will fail here with the offending
// sequence printed for reproduction.
func TestAdd_PropertyBufferAlwaysValid(t *testing.T) {
	type op struct {
		kind     string // "text" or "tool"
		turnID   string
		numTools int // for tool turns
	}
	sequences := [][]op{
		// Empty-ish and tiny sequences.
		{{kind: "text", turnID: "a"}},
		{{kind: "text", turnID: "a"}, {kind: "text", turnID: "b"}},
		{{kind: "tool", turnID: "a", numTools: 1}},
		// Alternating text and tool.
		{
			{kind: "text", turnID: "t1"},
			{kind: "tool", turnID: "u1", numTools: 1},
			{kind: "text", turnID: "t2"},
			{kind: "tool", turnID: "u2", numTools: 2},
			{kind: "text", turnID: "t3"},
		},
		// Long chain of small tool rounds — the classic #99 trigger.
		{
			{kind: "tool", turnID: "1", numTools: 1},
			{kind: "tool", turnID: "2", numTools: 2},
			{kind: "tool", turnID: "3", numTools: 1},
			{kind: "tool", turnID: "4", numTools: 1},
			{kind: "tool", turnID: "5", numTools: 1},
			{kind: "tool", turnID: "6", numTools: 3},
			{kind: "tool", turnID: "7", numTools: 1},
		},
		// Heavy tool usage — stress the soft-maxTurns overshoot and fallback.
		{
			{kind: "tool", turnID: "A", numTools: 5},
			{kind: "tool", turnID: "B", numTools: 5},
			{kind: "tool", turnID: "C", numTools: 5},
		},
		// Mixed with 10+ turns, varying shapes.
		{
			{kind: "text", turnID: "x1"},
			{kind: "tool", turnID: "x2", numTools: 1},
			{kind: "tool", turnID: "x3", numTools: 2},
			{kind: "text", turnID: "x4"},
			{kind: "tool", turnID: "x5", numTools: 1},
			{kind: "tool", turnID: "x6", numTools: 4},
			{kind: "text", turnID: "x7"},
			{kind: "text", turnID: "x8"},
			{kind: "tool", turnID: "x9", numTools: 1},
			{kind: "tool", turnID: "x10", numTools: 3},
			{kind: "text", turnID: "x11"},
			{kind: "tool", turnID: "x12", numTools: 2},
		},
		// Many short turns — exercises repeated eviction at the same boundary shape.
		{
			{kind: "tool", turnID: "s1", numTools: 1},
			{kind: "tool", turnID: "s2", numTools: 1},
			{kind: "tool", turnID: "s3", numTools: 1},
			{kind: "tool", turnID: "s4", numTools: 1},
			{kind: "tool", turnID: "s5", numTools: 1},
			{kind: "tool", turnID: "s6", numTools: 1},
			{kind: "tool", turnID: "s7", numTools: 1},
			{kind: "tool", turnID: "s8", numTools: 1},
			{kind: "tool", turnID: "s9", numTools: 1},
			{kind: "tool", turnID: "s10", numTools: 1},
		},
		// Clear + reuse.
		{
			{kind: "text", turnID: "c1"},
			{kind: "tool", turnID: "c2", numTools: 3},
			{kind: "text", turnID: "c3"},
		},
		// Single huge turn.
		{
			{kind: "tool", turnID: "solo", numTools: 5},
		},
	}

	for seqIdx, seq := range sequences {
		seqIdx, seq := seqIdx, seq
		t.Run(fmt.Sprintf("seq-%d", seqIdx), func(t *testing.T) {
			buf := ctxbuf.NewConversationBuffer(5) // small maxTurns to force eviction

			// Replay each operation Add-by-Add; after every single Add,
			// verify the invariant. This catches the case where the
			// intermediate state (mid-turn, before the final text) is
			// malformed even though the post-turn state would be valid.
			addOne := func(m anthropic.MessageParam) {
				buf.Add(m)
				assertValidSequence(t, buf.Messages())
				if t.Failed() {
					t.Logf("sequence %d, current buffer length=%d", seqIdx, len(buf.Messages()))
				}
			}

			for opIdx, o := range seq {
				switch o.kind {
				case "text":
					addOne(userMsg(o.turnID + "-q"))
					if t.Failed() {
						return
					}
					addOne(assistantMsg(o.turnID + "-a"))
				case "tool":
					addOne(userMsg(o.turnID + "-q"))
					for tIdx := 0; tIdx < o.numTools; tIdx++ {
						if t.Failed() {
							return
						}
						id := fmt.Sprintf("%s-tu-%d", o.turnID, tIdx)
						addOne(toolUseMsg(id, "kubectl_get"))
						if t.Failed() {
							return
						}
						addOne(toolResultMsg(id, "result"))
					}
					if t.Failed() {
						return
					}
					addOne(assistantMsg(o.turnID + "-a"))
				}
				if t.Failed() {
					t.Logf("sequence %d failed at op %d (%+v)", seqIdx, opIdx, o)
					return
				}
			}
		})
	}
}

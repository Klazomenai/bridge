package context_test

import (
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	ctxbuf "klazomenai/bridge/internal/context"
)

func userMsg(text string) anthropic.MessageParam {
	return anthropic.NewUserMessage(anthropic.NewTextBlock(text))
}

func assistantMsg(text string) anthropic.MessageParam {
	return anthropic.NewAssistantMessage(anthropic.NewTextBlock(text))
}

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
	// First message in buffer should be msg2, not msg1.
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

// Package context provides a thread-safe per-room conversation buffer.
// The buffer stores the last N turns of conversation to pass as the messages
// array in Anthropic API calls, enabling session-scoped memory within a walk.
// Buffers are in-memory only — they are cleared on pod restart (M2 will add
// Redis-backed persistence).
//
// # Invariant
//
// After every Add, the buffer is a valid Anthropic API messages array: every
// tool_result block in any message has the matching tool_use block in the
// immediately preceding assistant message. Equivalently, the buffer either
// starts with a user message with no tool_result blocks, or is empty. This
// protects against the "orphaned tool_result" 400 trap (see issue #99 in
// klazomenai/bridge).
//
// # Eviction and context loss
//
// To preserve the invariant, eviction does not split tool-use round-trips.
// It evicts whole turns by searching from the minimum-evict point forward
// for the next fresh Captain input (a user message with no tool_result
// blocks) and trimming everything before that boundary. If no such boundary
// exists at or after the minimum-evict point, the buffer is cleared rather
// than keeping an oversized prefix, even if the current start of the buffer
// is itself a valid boundary. This favours preserving the Anthropic
// messages-array invariant over retaining additional history; losing history
// is strictly better than producing a permanent 400 loop.
package context

import (
	"log/slog"
	"sync"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// DefaultMaxTurns is the default number of conversation turns to retain.
const DefaultMaxTurns = 10

// ConversationBuffer holds the last MaxTurns messages for a single room.
type ConversationBuffer struct {
	mu       sync.RWMutex
	maxTurns int
	messages []anthropic.MessageParam
}

// NewConversationBuffer returns a buffer with the given turn limit.
func NewConversationBuffer(maxTurns int) *ConversationBuffer {
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	return &ConversationBuffer{maxTurns: maxTurns}
}

// Add appends a message to the buffer and evicts oldest turns when over the
// size limit. Eviction walks forward to the first "safe boundary" — a fresh
// Captain user message with only text blocks — so tool_use/tool_result pairs
// are never split. See the package doc for the invariant.
//
// A post-condition guard enforces the invariant at the tail of the method:
// if the resulting buffer does not start with a user message with no
// tool_result blocks, it is cleared. This protects the invariant even if
// the caller breaks their protocol (e.g. adds an assistant message before
// a user message), and in particular prevents a cleared buffer from being
// repopulated with mid-turn tool messages that would leave the first entry
// malformed.
func (b *ConversationBuffer) Add(msg anthropic.MessageParam) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.messages = append(b.messages, msg)

	maxEntries := b.maxTurns * 2
	if len(b.messages) > maxEntries {
		minEvict := len(b.messages) - maxEntries
		boundary := findFirstSafeEvictionPoint(b.messages, minEvict)
		if boundary < 0 {
			// Entire buffer is one ongoing tool-use loop with no fresh Captain
			// input in sight. Dropping history is strictly better than
			// producing a malformed API messages array on the next call.
			// Logged so that monitoring can surface pathological cases.
			slog.Warn("context: no safe eviction boundary, clearing buffer",
				"size", len(b.messages), "max", maxEntries)
			b.messages = nil
		} else {
			b.messages = b.messages[boundary:]
		}
	}

	// Post-condition guard: the first message MUST be a user message with no
	// tool_result blocks, otherwise the buffer cannot be a prefix of a valid
	// Anthropic API messages array. Clear if violated.
	if len(b.messages) > 0 {
		first := b.messages[0]
		if first.Role != anthropic.MessageParamRoleUser || containsToolResult(first) {
			slog.Warn("context: buffer first message is not a user message with no tool_result blocks, clearing",
				"first_role", first.Role, "size", len(b.messages))
			b.messages = nil
		}
	}
}

// Messages returns a copy of the current buffer contents.
func (b *ConversationBuffer) Messages() []anthropic.MessageParam {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]anthropic.MessageParam, len(b.messages))
	copy(out, b.messages)
	return out
}

// Clear resets the buffer.
func (b *ConversationBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = nil
}

// containsToolResult reports whether msg carries any tool_result block. A
// user message with a tool_result cannot be a safe eviction boundary — its
// matching tool_use lives in the preceding message and must not be
// separated.
func containsToolResult(msg anthropic.MessageParam) bool {
	for i := range msg.Content {
		if msg.Content[i].OfToolResult != nil {
			return true
		}
	}
	return false
}

// findFirstSafeEvictionPoint returns the first index i >= minEvict where
// msgs[i] is a user message with no tool_result blocks — i.e. a fresh
// Captain input. Slicing msgs[i:] yields a valid Anthropic API messages
// array per the buffer invariant.
//
// Returns -1 if no such boundary exists at or after minEvict.
func findFirstSafeEvictionPoint(msgs []anthropic.MessageParam, minEvict int) int {
	for i := minEvict; i < len(msgs); i++ {
		if msgs[i].Role == anthropic.MessageParamRoleUser && !containsToolResult(msgs[i]) {
			return i
		}
	}
	return -1
}

// Manager maintains per-room ConversationBuffers.
type Manager struct {
	mu       sync.RWMutex
	maxTurns int
	rooms    map[string]*ConversationBuffer
}

// NewManager returns a Manager with the given per-room turn limit.
func NewManager(maxTurns int) *Manager {
	return &Manager{
		maxTurns: maxTurns,
		rooms:    make(map[string]*ConversationBuffer),
	}
}

// Buffer returns the ConversationBuffer for roomID, creating one if absent.
func (m *Manager) Buffer(roomID string) *ConversationBuffer {
	m.mu.RLock()
	buf, ok := m.rooms[roomID]
	m.mu.RUnlock()
	if ok {
		return buf
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check after acquiring write lock.
	if buf, ok = m.rooms[roomID]; ok {
		return buf
	}
	buf = NewConversationBuffer(m.maxTurns)
	m.rooms[roomID] = buf
	return buf
}

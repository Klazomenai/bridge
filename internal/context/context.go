// Package context provides a thread-safe per-room conversation buffer.
// The buffer stores the last N turns of conversation to pass as the messages
// array in Anthropic API calls, enabling session-scoped memory within a walk.
// Buffers are in-memory only — they are cleared on pod restart (M2 will add
// Redis-backed persistence).
package context

import (
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

// Add appends a message to the buffer, evicting the oldest turn-pair if over limit.
// Each call to Claude is a turn: one user message + one assistant message = 2 entries.
// We evict in pairs to maintain the alternating user/assistant invariant.
func (b *ConversationBuffer) Add(msg anthropic.MessageParam) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.messages = append(b.messages, msg)

	// Evict oldest pair when over limit (keep even number to preserve alternation).
	maxEntries := b.maxTurns * 2
	for len(b.messages) > maxEntries {
		b.messages = b.messages[2:]
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

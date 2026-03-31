package bot

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"klazomenai/bridge/internal/orchestrator"
)

const (
	// handleTimeout is the overall deadline for processing a message
	// (tool-use loops + delegations). Exceeding this sends a timeout
	// message to the room.
	handleTimeout = 120 * time.Second
	// typingTimeout is the duration sent with each typing indicator.
	// Matrix servers use this as a TTL; we refresh it before expiry.
	typingTimeout = 30 * time.Second
	// typingRefreshInterval is how often we resend the typing indicator.
	// Must be less than typingTimeout to prevent flicker.
	typingRefreshInterval = 25 * time.Second
	// typingCallTimeout is the per-call timeout for typing HTTP requests.
	// Typing is best-effort — a stalled request must not block response delivery.
	typingCallTimeout = 3 * time.Second
)

// handleMessage processes a decrypted incoming message event.
func (b *Bot) handleMessage(ctx context.Context, evt *event.Event) {
	// Ignore own messages.
	if evt.Sender == b.client.UserID {
		return
	}

	// Defense-in-depth: reject messages from rooms not on the allowlist.
	if !b.isRoomAllowed(evt.RoomID) {
		slog.Warn("bot: ignoring message from disallowed room", "room", evt.RoomID, "sender", evt.Sender)
		return
	}

	content := evt.Content.AsMessage()
	if content == nil || content.MsgType != event.MsgText {
		return
	}

	text := strings.TrimSpace(content.Body)
	if text == "" {
		return
	}

	requestedCrew := extractCrewRequest(text, b.cfg.KnownCrew)

	slog.Info("bot: message received",
		"room", evt.RoomID, "sender", evt.Sender,
		"crew_request", requestedCrew)

	handleCtx, cancel := context.WithTimeout(ctx, handleTimeout)
	defer cancel()

	// Run orchestrator in a goroutine; send typing indicator while waiting.
	ch := make(chan orchResult, 1)
	go func() {
		responses, err := b.orch.Handle(handleCtx, string(evt.RoomID), text, requestedCrew)
		ch <- orchResult{responses, err}
	}()

	b.awaitWithTyping(ctx, handleCtx, evt.RoomID, ch)
}

// orchResult holds the output of an async orchestrator call.
type orchResult struct {
	responses []orchestrator.Response
	err       error
}

// awaitWithTyping sends typing indicators while waiting for the orchestrator
// result, then sends responses or handles errors/timeouts.
// sendCtx is the parent (long-lived) context used for sending responses —
// separate from deadlineCtx so sends are not cut short by the handle timeout.
func (b *Bot) awaitWithTyping(sendCtx, deadlineCtx context.Context, roomID id.RoomID, ch <-chan orchResult) {
	// Start typing indicator (best-effort, short timeout).
	b.setTypingBestEffort(deadlineCtx, roomID, true)

	// Refresh typing before the TTL expires.
	ticker := time.NewTicker(typingRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case res := <-ch:
			// If the deadline fired at the same time, handle accordingly.
			if deadlineCtx.Err() != nil {
				if deadlineCtx.Err() == context.DeadlineExceeded {
					b.sendTimeout(sendCtx, roomID)
				} else {
					b.cancelTyping(sendCtx, roomID)
				}
				return
			}

			// Cancel typing indicator.
			b.cancelTyping(sendCtx, roomID)

			if res.err != nil {
				slog.Error("bot: orchestrator error", "room", roomID, "err", res.err)
				return
			}

			for i := range res.responses {
				resp := &res.responses[i]
				if err := b.sender.Send(sendCtx, roomID, resp); err != nil {
					slog.Error("bot: send failed", "room", roomID, "crew", resp.CrewID, "err", err)
				}
			}
			return

		case <-ticker.C:
			// Refresh typing indicator.
			b.setTypingBestEffort(deadlineCtx, roomID, true)

		case <-deadlineCtx.Done():
			if deadlineCtx.Err() == context.DeadlineExceeded {
				b.sendTimeout(sendCtx, roomID)
			} else {
				b.cancelTyping(sendCtx, roomID)
			}
			return
		}
	}
}

// sendTimeout cancels typing and sends the timeout message.
func (b *Bot) sendTimeout(ctx context.Context, roomID id.RoomID) {
	b.cancelTyping(ctx, roomID)
	slog.Warn("bot: message handling timed out", "room", roomID)
	timeoutResp := &orchestrator.Response{
		Text:      "The crew ran out of time on this one, Captain.",
		CrewID:    "bridge",
		Verbosity: "dispatch",
	}
	if err := b.sender.Send(ctx, roomID, timeoutResp); err != nil {
		slog.Error("bot: timeout message send failed", "room", roomID, "err", err)
	}
}

// setTypingBestEffort sends a typing indicator with a short timeout so a
// stalled homeserver doesn't block response delivery.
func (b *Bot) setTypingBestEffort(parent context.Context, roomID id.RoomID, typing bool) {
	ctx, cancel := context.WithTimeout(parent, typingCallTimeout)
	defer cancel()

	timeout := typingTimeout
	if !typing {
		timeout = 0
	}
	if err := b.typer.SetTyping(ctx, roomID, typing, timeout); err != nil {
		slog.Debug("bot: typing call failed", "room", roomID, "typing", typing, "err", err)
	}
}

// cancelTyping sends a typing=false indicator with a short timeout.
func (b *Bot) cancelTyping(ctx context.Context, roomID id.RoomID) {
	b.setTypingBestEffort(ctx, roomID, false)
}

// extractCrewRequest returns the lowercase crew ID if the message routes to a
// specific crew member, or "" to use the default.
// "over to <crew>" anywhere in the message takes precedence over a prefix match.
// knownCrew must be lowercase crew IDs from the loaded registry.
func extractCrewRequest(text string, knownCrew []string) string {
	lower := strings.ToLower(text)

	// "Over to <crew>" overrides prefix routing — check this first.
	// When multiple "over to" mentions exist, pick the earliest match in the
	// message so routing is deterministic regardless of knownCrew slice order.
	bestIdx := -1
	bestCrew := ""
	for _, c := range knownCrew {
		phrase := "over to " + c
		idx := strings.Index(lower, phrase)
		if idx == -1 {
			continue
		}
		after := idx + len(phrase)
		if after == len(lower) || !isWordChar(rune(lower[after])) {
			if bestIdx == -1 || idx < bestIdx {
				bestIdx = idx
				bestCrew = c
			}
		}
	}
	if bestCrew != "" {
		return bestCrew
	}

	// Prefix routing: "<crew>," or "<crew>:".
	for _, c := range knownCrew {
		if strings.HasPrefix(lower, c+",") || strings.HasPrefix(lower, c+":") {
			return c
		}
	}

	return ""
}

// isWordChar reports whether r is a letter or digit (ASCII).
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

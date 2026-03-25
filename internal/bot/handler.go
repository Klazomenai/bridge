package bot

import (
	"context"
	"log/slog"
	"strings"

	"maunium.net/go/mautrix/event"
)

// handleMessage processes a decrypted incoming message event.
func (b *Bot) handleMessage(ctx context.Context, evt *event.Event) {
	// Ignore own messages.
	if evt.Sender == b.client.UserID {
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

	responses, err := b.orch.Handle(ctx, string(evt.RoomID), text, requestedCrew)
	if err != nil {
		slog.Error("bot: orchestrator error", "err", err)
		return
	}

	for _, resp := range responses {
		if err := b.sender.Send(ctx, evt.RoomID, &resp); err != nil {
			slog.Error("bot: send failed", "room", evt.RoomID, "crew", resp.CrewID, "err", err)
		}
	}
}

// extractCrewRequest returns the lowercase crew ID if the message routes to a
// specific crew member, or "" to use the default.
// "over to <crew>" anywhere in the message takes precedence over a prefix match.
// knownCrew must be lowercase crew IDs from the loaded registry.
func extractCrewRequest(text string, knownCrew []string) string {
	lower := strings.ToLower(text)

	// "Over to <crew>" overrides prefix routing — check this first.
	// Require a word boundary after the crew ID to avoid matching partial words
	// (e.g. "crest" must not match "over to crestfallen").
	for _, c := range knownCrew {
		phrase := "over to " + c
		idx := strings.Index(lower, phrase)
		if idx == -1 {
			continue
		}
		after := idx + len(phrase)
		if after == len(lower) || !isWordChar(rune(lower[after])) {
			return c
		}
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

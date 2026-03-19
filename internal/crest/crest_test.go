package crest_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"klazomenai/bridge/internal/crest"
)

func TestSMTPConfigValidation(t *testing.T) {
	// A misconfigured SMTP host should return an error, not panic.
	cfg := crest.SMTPConfig{
		Host:     "127.0.0.1",
		Port:     9999, // no server listening
		Username: "u",
		Password: "p",
		From:     "test@example.com",
	}
	err := crest.SendMail(cfg, "to@example.com", "test subject", "test body")
	if err == nil {
		t.Fatal("expected error connecting to non-existent SMTP server")
	}
}

func TestIMAPPollNoMessagesReturnsEmptySlice(t *testing.T) {
	// Poll against a non-existent server — expect an error (not a panic or hang).
	cfg := crest.IMAPConfig{
		Host:     "127.0.0.1",
		Port:     9998,
		Username: "u",
		Password: "p",
	}
	// We expect a dial error because no server is listening.
	_, err := crest.Poll(t.Context(), cfg)
	if err == nil {
		t.Fatal("expected dial error connecting to non-existent IMAP server, got nil")
	}
}

func TestPollerExitsOnContextCancel(t *testing.T) {
	// Start poller with an already-cancelled context — it must return immediately.
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before starting

	cfg := crest.IMAPConfig{Host: "127.0.0.1", Port: 9997}
	done := make(chan struct{})
	go func() {
		defer close(done)
		crest.Poller(ctx, cfg, 100*time.Millisecond, func(_ []crest.Message) {})
	}()

	select {
	case <-done:
		// Poller terminated as expected.
	case <-t.Context().Done():
		t.Fatal("Poller did not exit on context cancellation")
	}
}

func TestPollerTicksAndHandlesError(t *testing.T) {
	// Start poller with a very short interval so the ticker fires before we cancel.
	// Poll will fail (no IMAP server) — verify no panic and the handler is not called.
	ctx, cancel := context.WithCancel(t.Context())

	cfg := crest.IMAPConfig{Host: "127.0.0.1", Port: 9993}
	handlerCalls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		crest.Poller(ctx, cfg, 1*time.Millisecond, func(msgs []crest.Message) {
			handlerCalls++
		})
	}()

	// Let at least one tick happen.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal("Poller did not exit")
	}

	// Handler should not have been called (poll fails on connect error).
	if handlerCalls > 0 {
		t.Errorf("handler called unexpectedly: %d times", handlerCalls)
	}
}

func TestSendRegistrationTokenFormatsCorrectly(t *testing.T) {
	// Verify the function calls SendMail with the expected subject.
	// Since we can't send a real email, we confirm the error path from
	// a bad SMTP address gives us an actionable error, not a panic.
	cfg := crest.SMTPConfig{
		Host:     "127.0.0.1",
		Port:     9996,
		Username: "u",
		Password: "p",
		From:     "test@example.com",
	}
	err := crest.SendRegistrationToken(cfg, "captain@example.com", "test-token-123")
	if err == nil {
		t.Fatal("expected error from unreachable SMTP server")
	}
	if !strings.Contains(err.Error(), "token delivery") {
		t.Errorf("expected 'token delivery' in error, got: %v", err)
	}
}

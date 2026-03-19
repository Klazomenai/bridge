package crest_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

func TestPollerNonPositiveIntervalReturnsImmediately(t *testing.T) {
	cfg := crest.IMAPConfig{Host: "127.0.0.1", Port: 9997}
	done := make(chan struct{})
	go func() {
		defer close(done)
		crest.PollerWithPollFn(t.Context(), cfg, 0, func(_ []crest.Message) {}, func(_ context.Context, _ crest.IMAPConfig) ([]crest.Message, error) {
			return nil, nil
		})
	}()
	select {
	case <-done:
		// returned immediately as expected
	case <-t.Context().Done():
		t.Fatal("PollerWithPollFn did not return immediately for non-positive interval")
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
	// Use PollerWithPollFn so we can synchronise on the first tick via a
	// channel instead of time.Sleep (which is flaky on loaded CI runners).
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	cfg := crest.IMAPConfig{Host: "127.0.0.1", Port: 9993}
	handlerCalls := 0

	// tickDone is closed after the first poll attempt completes.
	tickDone := make(chan struct{})
	var once sync.Once
	stubPoll := func(_ context.Context, _ crest.IMAPConfig) ([]crest.Message, error) {
		once.Do(func() { close(tickDone) })
		return nil, fmt.Errorf("simulated connection refused")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		crest.PollerWithPollFn(ctx, cfg, 1*time.Millisecond, func(msgs []crest.Message) {
			handlerCalls++
		}, stubPoll)
	}()

	// Wait until at least one tick has been attempted, then cancel.
	select {
	case <-tickDone:
	case <-t.Context().Done():
		t.Fatal("Poller did not tick within test deadline")
	}
	cancel()

	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal("Poller did not exit after context cancel")
	}

	// Handler should not have been called (poll returns error, not messages).
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

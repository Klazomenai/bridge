package crest_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	basecrest "klazomenai/bridge/internal/crest"
	cresttools "klazomenai/bridge/internal/tools/crest"
)

// --- imap_poll tests ---

func mockPoll(msgs []basecrest.Message, err error) basecrest.PollFn {
	return func(_ context.Context, _ basecrest.IMAPConfig) ([]basecrest.Message, error) {
		return msgs, err
	}
}

func TestIMAPPollNoMessages(t *testing.T) {
	tool := cresttools.NewIMAPPollToolWithFn(basecrest.IMAPConfig{}, mockPoll(nil, nil))

	result, err := tool.Execute(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "[]" {
		t.Errorf("expected empty JSON array, got: %q", result)
	}
}

func TestIMAPPollWithMessages(t *testing.T) {
	msgs := []basecrest.Message{
		{From: "alice@example.com", Subject: "Hello", Body: "Short body."},
		{From: "bob@example.com", Subject: "Update", Body: "Another message."},
	}
	tool := cresttools.NewIMAPPollToolWithFn(basecrest.IMAPConfig{}, mockPoll(msgs, nil))

	result, err := tool.Execute(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed []struct {
		From        string `json:"from"`
		Subject     string `json:"subject"`
		BodyPreview string `json:"body_preview"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(parsed))
	}
	if parsed[0].From != "alice@example.com" {
		t.Errorf("expected alice, got %q", parsed[0].From)
	}
	if parsed[0].BodyPreview != "Short body." {
		t.Errorf("expected full body for short message, got %q", parsed[0].BodyPreview)
	}
}

func TestIMAPPollBodyTruncated(t *testing.T) {
	longBody := strings.Repeat("a", 300)
	msgs := []basecrest.Message{
		{From: "x@x.com", Subject: "Long", Body: longBody},
	}
	tool := cresttools.NewIMAPPollToolWithFn(basecrest.IMAPConfig{}, mockPoll(msgs, nil))

	result, err := tool.Execute(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed []struct {
		BodyPreview string `json:"body_preview"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatal(err)
	}
	// 200 chars + "..."
	if len([]rune(parsed[0].BodyPreview)) != 203 {
		t.Errorf("expected 203 rune preview, got %d", len([]rune(parsed[0].BodyPreview)))
	}
	if !strings.HasSuffix(parsed[0].BodyPreview, "...") {
		t.Error("expected ... suffix on truncated preview")
	}
}

func TestIMAPPollCustomMailbox(t *testing.T) {
	var capturedCfg basecrest.IMAPConfig
	tool := cresttools.NewIMAPPollToolWithFn(basecrest.IMAPConfig{Mailbox: "INBOX"},
		func(_ context.Context, cfg basecrest.IMAPConfig) ([]basecrest.Message, error) {
			capturedCfg = cfg
			return nil, nil
		})

	_, err := tool.Execute(t.Context(), json.RawMessage(`{"mailbox":"Sent"}`))
	if err != nil {
		t.Fatal(err)
	}
	if capturedCfg.Mailbox != "Sent" {
		t.Errorf("expected Sent mailbox, got %q", capturedCfg.Mailbox)
	}
}

func TestIMAPPollError(t *testing.T) {
	tool := cresttools.NewIMAPPollToolWithFn(basecrest.IMAPConfig{},
		mockPoll(nil, errors.New("connection refused")))

	_, err := tool.Execute(t.Context(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestIMAPPollName(t *testing.T) {
	tool := cresttools.NewIMAPPollTool(basecrest.IMAPConfig{})
	if tool.Name() != "imap_poll" {
		t.Errorf("expected imap_poll, got %q", tool.Name())
	}
}

// --- smtp_send tests ---

type sendRecord struct {
	to      string
	subject string
	body    string
}

func mockSend(records *[]sendRecord, err error) cresttools.SendFn {
	return func(_ basecrest.SMTPConfig, to, subject, body string) error {
		if records != nil {
			*records = append(*records, sendRecord{to, subject, body})
		}
		return err
	}
}

func TestSMTPSendSuccess(t *testing.T) {
	var records []sendRecord
	tool := cresttools.NewSMTPSendToolWithFn(basecrest.SMTPConfig{},
		"captain@ship.dev", mockSend(&records, nil))

	result, err := tool.Execute(t.Context(),
		json.RawMessage(`{"to":"captain@ship.dev","subject":"Report","body":"All clear."}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "captain@ship.dev") {
		t.Errorf("expected confirmation with recipient, got: %q", result)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 send, got %d", len(records))
	}
	if records[0].subject != "Report" {
		t.Errorf("expected subject Report, got %q", records[0].subject)
	}
}

func TestSMTPSendAllowlistRejection(t *testing.T) {
	tool := cresttools.NewSMTPSendToolWithFn(basecrest.SMTPConfig{},
		"allowed@ship.dev", mockSend(nil, nil))

	_, err := tool.Execute(t.Context(),
		json.RawMessage(`{"to":"hacker@evil.com","subject":"x","body":"x"}`))
	if err == nil {
		t.Fatal("expected allowlist rejection")
	}
	if !strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSMTPSendAllowlistCaseInsensitive(t *testing.T) {
	var records []sendRecord
	tool := cresttools.NewSMTPSendToolWithFn(basecrest.SMTPConfig{},
		"Captain@Ship.Dev", mockSend(&records, nil))

	_, err := tool.Execute(t.Context(),
		json.RawMessage(`{"to":"captain@ship.dev","subject":"x","body":"x"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatal("expected send to succeed (case-insensitive match)")
	}
}

func TestSMTPSendEmptyAllowlistDeniesAll(t *testing.T) {
	tool := cresttools.NewSMTPSendToolWithFn(basecrest.SMTPConfig{},
		"", mockSend(nil, nil))

	_, err := tool.Execute(t.Context(),
		json.RawMessage(`{"to":"anyone@anywhere.com","subject":"x","body":"x"}`))
	if err == nil {
		t.Fatal("expected allowlist rejection with empty allowlist")
	}
	if !strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSMTPSendRateLimit(t *testing.T) {
	tool := cresttools.NewSMTPSendToolWithFn(basecrest.SMTPConfig{},
		"x@x.com", mockSend(nil, nil))

	// Send maxSendsPerHour (5) times — all should succeed.
	for i := range 5 {
		_, err := tool.Execute(t.Context(),
			json.RawMessage(`{"to":"x@x.com","subject":"x","body":"x"}`))
		if err != nil {
			t.Fatalf("send %d should succeed: %v", i+1, err)
		}
	}

	// 6th send should be rate-limited.
	_, err := tool.Execute(t.Context(),
		json.RawMessage(`{"to":"x@x.com","subject":"x","body":"x"}`))
	if err == nil {
		t.Fatal("expected rate limit error on 6th send")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSMTPSendMissingFields(t *testing.T) {
	tool := cresttools.NewSMTPSendToolWithFn(basecrest.SMTPConfig{},
		"x@x.com", mockSend(nil, nil))

	cases := []struct {
		name  string
		input string
	}{
		{"missing to", `{"subject":"x","body":"x"}`},
		{"missing subject", `{"to":"x@x.com","body":"x"}`},
		{"missing body", `{"to":"x@x.com","subject":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(t.Context(), json.RawMessage(tc.input))
			if err == nil {
				t.Fatal("expected error for missing field")
			}
		})
	}
}

func TestSMTPSendError(t *testing.T) {
	tool := cresttools.NewSMTPSendToolWithFn(basecrest.SMTPConfig{},
		"x@x.com", mockSend(nil, errors.New("smtp timeout")))

	_, err := tool.Execute(t.Context(),
		json.RawMessage(`{"to":"x@x.com","subject":"x","body":"x"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "smtp timeout") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSMTPSendName(t *testing.T) {
	tool := cresttools.NewSMTPSendTool(basecrest.SMTPConfig{}, "")
	if tool.Name() != "smtp_send" {
		t.Errorf("expected smtp_send, got %q", tool.Name())
	}
}

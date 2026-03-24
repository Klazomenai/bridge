package crest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type pollResult struct {
	Messages []struct {
		From        string `json:"from"`
		Subject     string `json:"subject"`
		BodyPreview string `json:"body_preview"`
	} `json:"messages"`
	Total   int `json:"total"`
	Showing int `json:"showing"`
}

func parsePollResult(t *testing.T, result string) pollResult {
	t.Helper()
	var parsed pollResult
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("failed to parse result JSON: %v\nraw: %s", err, result)
	}
	return parsed
}

func TestIMAPPollNoMessages(t *testing.T) {
	tool := cresttools.NewIMAPPollToolWithFn(basecrest.IMAPConfig{}, mockPoll(nil, nil))

	result, err := tool.Execute(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parsePollResult(t, result)
	if len(parsed.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(parsed.Messages))
	}
	if parsed.Total != 0 || parsed.Showing != 0 {
		t.Errorf("expected total=0 showing=0, got total=%d showing=%d", parsed.Total, parsed.Showing)
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
	parsed := parsePollResult(t, result)
	if len(parsed.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(parsed.Messages))
	}
	if parsed.Total != 2 || parsed.Showing != 2 {
		t.Errorf("expected total=2 showing=2, got total=%d showing=%d", parsed.Total, parsed.Showing)
	}
	if parsed.Messages[0].From != "alice@example.com" {
		t.Errorf("expected alice, got %q", parsed.Messages[0].From)
	}
	if parsed.Messages[0].BodyPreview != "Short body." {
		t.Errorf("expected full body for short message, got %q", parsed.Messages[0].BodyPreview)
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
	parsed := parsePollResult(t, result)
	// 200 chars + "..."
	preview := parsed.Messages[0].BodyPreview
	if len([]rune(preview)) != 203 {
		t.Errorf("expected 203 rune preview, got %d", len([]rune(preview)))
	}
	if !strings.HasSuffix(preview, "...") {
		t.Error("expected ... suffix on truncated preview")
	}
}

func TestIMAPPollMessageCountCapped(t *testing.T) {
	// Create 15 messages — should be capped to 10.
	msgs := make([]basecrest.Message, 15)
	for i := range msgs {
		msgs[i] = basecrest.Message{
			From:    fmt.Sprintf("user%d@example.com", i),
			Subject: fmt.Sprintf("Message %d", i),
			Body:    "body",
		}
	}
	tool := cresttools.NewIMAPPollToolWithFn(basecrest.IMAPConfig{}, mockPoll(msgs, nil))

	result, err := tool.Execute(t.Context(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parsePollResult(t, result)
	if parsed.Showing != 10 {
		t.Errorf("expected showing=10, got %d", parsed.Showing)
	}
	if parsed.Total != 15 {
		t.Errorf("expected total=15, got %d", parsed.Total)
	}
	if len(parsed.Messages) != 10 {
		t.Errorf("expected 10 messages, got %d", len(parsed.Messages))
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

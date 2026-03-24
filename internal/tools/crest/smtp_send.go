package crest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	basecrest "klazomenai/bridge/internal/crest"
)

// SendFn is the signature for the email send function.
// Exposed so tests can inject a stub.
type SendFn func(cfg basecrest.SMTPConfig, to, subject, body string) error

// SMTPSendTool sends a plain-text email via the configured SMTP relay.
type SMTPSendTool struct {
	cfg       basecrest.SMTPConfig
	allowlist map[string]bool
	sendFn    SendFn

	mu        sync.Mutex
	sendCount int
	windowEnd time.Time
}

const (
	maxSendsPerHour = 5
	rateLimitWindow = time.Hour
)

// NewSMTPSendTool creates an smtp_send tool with allowlist enforcement and rate limiting.
// allowlist is a comma-separated list of allowed recipient addresses.
func NewSMTPSendTool(cfg basecrest.SMTPConfig, allowlist string) *SMTPSendTool {
	allowed := make(map[string]bool)
	for _, addr := range strings.Split(allowlist, ",") {
		addr = strings.TrimSpace(strings.ToLower(addr))
		if addr != "" {
			allowed[addr] = true
		}
	}
	return &SMTPSendTool{
		cfg:       cfg,
		allowlist: allowed,
		sendFn:    basecrest.SendMail,
	}
}

// NewSMTPSendToolWithFn creates an smtp_send tool with a custom send function (for testing).
func NewSMTPSendToolWithFn(cfg basecrest.SMTPConfig, allowlist string, fn SendFn) *SMTPSendTool {
	t := NewSMTPSendTool(cfg, allowlist)
	t.sendFn = fn
	return t
}

func (t *SMTPSendTool) Name() string        { return "smtp_send" }
func (t *SMTPSendTool) Description() string { return "Send a plain-text email to a recipient." }

func (t *SMTPSendTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"to": map[string]any{
				"type":        "string",
				"description": "Recipient email address",
			},
			"subject": map[string]any{
				"type":        "string",
				"description": "Email subject",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Plain text email body",
			},
		},
		Required: []string{"to", "subject", "body"},
	}
}

type smtpSendInput struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (t *SMTPSendTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var params smtpSendInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.To == "" || params.Subject == "" || params.Body == "" {
		return "", fmt.Errorf("to, subject, and body are required")
	}

	// Normalise and validate recipient.
	recipient := strings.TrimSpace(strings.ToLower(params.To))
	if recipient == "" {
		return "", fmt.Errorf("to address is empty after normalisation")
	}

	// Allowlist enforcement (default deny — empty allowlist rejects all).
	if !t.allowlist[recipient] {
		slog.Warn("crest: smtp_send rejected — recipient not in allowlist",
			"to", recipient)
		return "", fmt.Errorf("recipient %q not in allowlist", recipient)
	}

	// Rate limiting (pre-check only — count incremented after successful send).
	if err := t.checkRateLimit(); err != nil {
		return "", err
	}

	if err := t.sendFn(t.cfg, recipient, params.Subject, params.Body); err != nil {
		return "", err
	}

	t.incrementSendCount()
	slog.Info("crest: email sent", "to", recipient)
	return fmt.Sprintf("Email sent to %s.", recipient), nil
}

// checkRateLimit checks (but does not increment) the per-hour send cap.
func (t *SMTPSendTool) checkRateLimit() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	if now.After(t.windowEnd) {
		t.sendCount = 0
		t.windowEnd = now.Add(rateLimitWindow)
	}

	if t.sendCount >= maxSendsPerHour {
		return fmt.Errorf("rate limit exceeded: %d sends per hour", maxSendsPerHour)
	}
	return nil
}

// incrementSendCount records a successful send against the rate limit.
func (t *SMTPSendTool) incrementSendCount() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sendCount++
}

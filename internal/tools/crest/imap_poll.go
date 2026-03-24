// Package crest provides Crest's Signalman tools: email polling and sending.
package crest

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	basecrest "klazomenai/bridge/internal/crest"
)

// IMAPPollTool checks the configured mailbox for unseen messages.
type IMAPPollTool struct {
	cfg    basecrest.IMAPConfig
	pollFn basecrest.PollFn
}

// NewIMAPPollTool creates an imap_poll tool with the given IMAP config.
func NewIMAPPollTool(cfg basecrest.IMAPConfig) *IMAPPollTool {
	return &IMAPPollTool{cfg: cfg, pollFn: basecrest.Poll}
}

// NewIMAPPollToolWithFn creates an imap_poll tool with a custom poll function (for testing).
func NewIMAPPollToolWithFn(cfg basecrest.IMAPConfig, fn basecrest.PollFn) *IMAPPollTool {
	return &IMAPPollTool{cfg: cfg, pollFn: fn}
}

func (t *IMAPPollTool) Name() string        { return "imap_poll" }
func (t *IMAPPollTool) Description() string { return "Check the email inbox for unseen messages." }

func (t *IMAPPollTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"mailbox": map[string]any{
				"type":        "string",
				"description": "IMAP mailbox to check (default: INBOX)",
			},
		},
	}
}

type imapPollInput struct {
	Mailbox string `json:"mailbox"`
}

type imapPollMessage struct {
	From        string `json:"from"`
	Subject     string `json:"subject"`
	BodyPreview string `json:"body_preview"`
}

func (t *IMAPPollTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params imapPollInput
	if len(input) > 0 {
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("invalid input: %w", err)
		}
	}

	cfg := t.cfg
	if params.Mailbox != "" {
		cfg.Mailbox = params.Mailbox
	}

	msgs, err := t.pollFn(ctx, cfg)
	if err != nil {
		return "", err
	}

	if len(msgs) == 0 {
		return "No unseen messages.", nil
	}

	results := make([]imapPollMessage, len(msgs))
	for i, m := range msgs {
		body := m.Body
		if len([]rune(body)) > 200 {
			body = string([]rune(body)[:200]) + "..."
		}
		results[i] = imapPollMessage{
			From:        m.From,
			Subject:     m.Subject,
			BodyPreview: body,
		}
	}

	out, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("marshal results: %w", err)
	}
	return string(out), nil
}

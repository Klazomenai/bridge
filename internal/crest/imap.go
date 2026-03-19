package crest

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// IMAPConfig holds connection details for the ProtonMail bridge IMAP endpoint.
type IMAPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	Mailbox  string
}

// Message is a simplified representation of an IMAP message.
type Message struct {
	UID     imap.UID
	From    string
	Subject string
	Body    string
}

// Poll connects to the IMAP server, fetches unseen messages from the configured
// mailbox, and returns them. The caller is responsible for marking them as seen.
//
// TODO(M2): imapclient.DialInsecure has no context parameter, so ctx cancellation
// cannot abort in-flight dial/login/fetch calls. Add a net.Dialer timeout wrapper
// when upgrading to a go-imap/v2 release that exposes context-aware dialling.
func Poll(ctx context.Context, cfg IMAPConfig) ([]Message, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// ProtonMail bridge uses plain (non-TLS) on localhost — TLS is handled by the bridge.
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		return nil, fmt.Errorf("imap dial %s: %w", addr, err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Login(cfg.Username, cfg.Password).Wait(); err != nil {
		return nil, fmt.Errorf("imap login: %w", err)
	}

	mailbox := cfg.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}

	if _, err := c.Select(mailbox, nil).Wait(); err != nil {
		return nil, fmt.Errorf("imap select %s: %w", mailbox, err)
	}

	// Search for unseen messages.
	searchData, err := c.Search(&imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap search: %w", err)
	}

	seqNums := searchData.AllSeqNums()
	if len(seqNums) == 0 {
		slog.Info("crest: no new signals")
		return nil, nil
	}

	seqSet := imap.SeqSetNum(seqNums...)
	fetchOptions := &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierText},
		},
	}

	msgs, err := c.Fetch(seqSet, fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap fetch: %w", err)
	}

	var result []Message
	for _, msg := range msgs {
		m := Message{UID: msg.UID}
		if msg.Envelope != nil {
			m.Subject = msg.Envelope.Subject
			if len(msg.Envelope.From) > 0 {
				m.From = msg.Envelope.From[0].Addr()
			}
		}
		for _, section := range msg.BodySection {
			m.Body = string(section.Bytes)
		}
		result = append(result, m)
	}

	slog.Info("crest: new signals received", "count", len(result))
	return result, nil
}

// PollFn is the signature of a poll function used by PollerWithPollFn.
// Exposed so tests can inject a synchronisable stub without time.Sleep.
type PollFn func(ctx context.Context, cfg IMAPConfig) ([]Message, error)

// Poller runs Poll on a fixed interval until ctx is cancelled.
func Poller(ctx context.Context, cfg IMAPConfig, interval time.Duration, handler func([]Message)) {
	PollerWithPollFn(ctx, cfg, interval, handler, Poll)
}

// PollerWithPollFn is like Poller but accepts a custom poll function.
// Use this in tests to replace Poll with a stub that signals via a channel,
// making the test deterministic without time.Sleep.
func PollerWithPollFn(ctx context.Context, cfg IMAPConfig, interval time.Duration, handler func([]Message), pollFn PollFn) {
	if interval <= 0 {
		slog.Error("crest: poller interval must be positive", "interval", interval)
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msgs, err := pollFn(ctx, cfg)
			if err != nil {
				slog.Error("crest: imap poll failed", "err", err)
				continue
			}
			if len(msgs) > 0 {
				handler(msgs)
			}
		}
	}
}

// Package bot implements the mautrix-go Matrix bot client.
// Built with -tags goolm (pure-Go olm, no CGo) so the image can use distroless/static.
// E2EE session state is persisted to /data/crypto-store (PVC-backed SQLite).
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/util/dbutil"
	_ "modernc.org/sqlite" // pure-Go SQLite driver, no CGo required

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"klazomenai/bridge/internal/orchestrator"
)

// OrchestratorI is the subset of orchestrator.Orchestrator used by the bot.
// Keeping it as an interface allows injection of test doubles.
type OrchestratorI interface {
	Handle(ctx context.Context, roomID, userText, requestedCrew string) (*orchestrator.Response, error)
}

// Sender abstracts the Matrix message-sending operation.
type Sender interface {
	Send(ctx context.Context, roomID id.RoomID, resp *orchestrator.Response) error
}

// Config holds bot connection parameters.
type Config struct {
	Homeserver   string
	Username     string
	Password     string
	CryptoDBPath string // path to SQLite DB for E2EE key persistence (PVC)
	PickleKey    string // secret used to encrypt the olm account on disk
	DisplayName  string
}

// Bot is the mautrix-go Matrix bot.
type Bot struct {
	client *mautrix.Client
	helper *cryptohelper.CryptoHelper
	orch   OrchestratorI
	sender Sender
	cfg    Config
}

// New creates a Bot but does not connect yet.
func New(cfg Config, orch OrchestratorI) (*Bot, error) {
	if cfg.CryptoDBPath == "" {
		cfg.CryptoDBPath = "/data/crypto-store/bridge.db"
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "Bridge"
	}

	client, err := mautrix.NewClient(cfg.Homeserver, "", "")
	if err != nil {
		return nil, fmt.Errorf("mautrix client: %w", err)
	}

	b := &Bot{client: client, orch: orch, cfg: cfg}
	b.sender = &matrixSender{client: client}
	return b, nil
}

// Start logs in, initialises E2EE, and begins syncing.
// It blocks until ctx is cancelled.
func (b *Bot) Start(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(b.cfg.CryptoDBPath), 0o700); err != nil {
		return fmt.Errorf("create crypto store dir: %w", err)
	}

	// Open pure-Go SQLite for the crypto store (CGO_ENABLED=0 compatible).
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", b.cfg.CryptoDBPath)
	db, err := dbutil.NewWithDialect(dsn, "sqlite")
	if err != nil {
		return fmt.Errorf("open crypto store db: %w", err)
	}

	pickleKey := []byte(b.cfg.PickleKey)
	helper, err := cryptohelper.NewCryptoHelper(b.client, pickleKey, db)
	if err != nil {
		return fmt.Errorf("crypto helper: %w", err)
	}
	// LoginAs: the helper will log in on first run and reuse the stored
	// device ID + access token on subsequent starts (E2EE key persistence).
	helper.LoginAs = &mautrix.ReqLogin{
		Type:                     mautrix.AuthTypePassword,
		Identifier:               mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: b.cfg.Username},
		Password:                 b.cfg.Password,
		InitialDeviceDisplayName: b.cfg.DisplayName,
	}
	b.helper = helper

	if err := helper.Init(ctx); err != nil {
		return fmt.Errorf("crypto helper init: %w", err)
	}
	b.client.Crypto = helper

	b.registerHandlers()

	slog.Info("bot: sync starting", "user", b.client.UserID)
	if err := b.client.SyncWithContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}

// registerHandlers wires up event handlers on the syncer.
func (b *Bot) registerHandlers() {
	syncer := b.client.Syncer.(*mautrix.DefaultSyncer)

	// Auto-accept invites.
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		if evt.GetStateKey() == b.client.UserID.String() &&
			evt.Content.AsMember().Membership == event.MembershipInvite {
			if _, err := b.client.JoinRoomByID(ctx, evt.RoomID); err != nil {
				slog.Error("bot: failed to join room", "room", evt.RoomID, "err", err)
			} else {
				slog.Info("bot: joined room", "room", evt.RoomID)
			}
		}
	})

	// Handle incoming text messages (decrypted by the crypto helper automatically).
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		b.handleMessage(ctx, evt)
	})
}

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

	requestedCrew := extractCrewRequest(text)

	slog.Info("bot: message received",
		"room", evt.RoomID, "sender", evt.Sender,
		"crew_request", requestedCrew)

	resp, err := b.orch.Handle(ctx, string(evt.RoomID), text, requestedCrew)
	if err != nil {
		slog.Error("bot: orchestrator error", "err", err)
		return
	}

	if err := b.sender.Send(ctx, evt.RoomID, resp); err != nil {
		slog.Error("bot: send failed", "room", evt.RoomID, "err", err)
	}
}

// matrixSender is the production Sender backed by a real mautrix client.
type matrixSender struct {
	client *mautrix.Client
}

func (s *matrixSender) Send(ctx context.Context, roomID id.RoomID, resp *orchestrator.Response) error {
	_, err := s.client.SendMessageEvent(ctx, roomID, event.EventMessage, struct {
		MsgType    event.MessageType `json:"msgtype"`
		Body       string            `json:"body"`
		CrewMember string            `json:"crew_member"`
		Verbosity  string            `json:"verbosity"`
	}{
		MsgType:    event.MsgText,
		Body:       resp.Text,
		CrewMember: resp.CrewID,
		Verbosity:  resp.Verbosity,
	})
	return err
}

// extractCrewRequest returns the lowercase crew ID if the message routes to a
// specific crew member, or "" to use the default.
// "over to <crew>" anywhere in the message takes precedence over a prefix match.
func extractCrewRequest(text string) string {
	lower := strings.ToLower(text)

	// "Over to <crew>" overrides prefix routing — check this first.
	for _, c := range []string{"maren", "crest", "rhys", "finn", "sable", "vesper"} {
		if strings.Contains(lower, "over to "+c) {
			return c
		}
	}

	// Prefix routing: "<crew>," or "<crew>:".
	for _, c := range []string{"maren", "crest", "rhys", "finn", "sable", "vesper"} {
		if strings.HasPrefix(lower, c+",") || strings.HasPrefix(lower, c+":") {
			return c
		}
	}

	return ""
}

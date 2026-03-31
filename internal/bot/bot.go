// Package bot implements the mautrix-go Matrix bot client.
// Built with -tags goolm (pure-Go olm, no CGo) so the image can use distroless/static.
// E2EE session state is persisted to /var/lib/bridge (PVC-backed SQLite).
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

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
	Handle(ctx context.Context, roomID, userText, requestedCrew string) ([]orchestrator.Response, error)
}

// DefaultCryptoDBPath is the default path for the E2EE crypto store SQLite DB.
// It must match the PVC mount path configured in the Helm chart (cryptoStore.mountPath).
const DefaultCryptoDBPath = "/var/lib/bridge/bridge.db"

// Config holds bot connection parameters.
type Config struct {
	Homeserver    string
	Username      string
	Password      string
	CryptoDBPath  string              // path to SQLite DB for E2EE key persistence (PVC)
	PickleKey     string              // secret used to encrypt the olm account on disk
	DisplayName   string
	KnownCrew     []string            // crew IDs loaded from registry — used for routing
	RoomAllowlist map[id.RoomID]struct{} // permitted rooms — empty = deny all (fail-closed)
}

// Bot is the mautrix-go Matrix bot.
type Bot struct {
	client  *mautrix.Client
	helper  *cryptohelper.CryptoHelper
	orch    OrchestratorI
	sender  Sender
	typer   Typer
	cfg     Config
	OnReady func() // called after crypto + handlers are initialised, before the sync loop starts
}

// New creates a Bot but does not connect yet.
func New(cfg Config, orch OrchestratorI) (*Bot, error) {
	if cfg.CryptoDBPath == "" {
		cfg.CryptoDBPath = DefaultCryptoDBPath
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "Bridge"
	}

	slog.Info("bot: room allowlist", "count", len(cfg.RoomAllowlist))
	if len(cfg.RoomAllowlist) == 0 {
		slog.Warn("bot: room allowlist is empty — all invites will be rejected")
	}

	client, err := mautrix.NewClient(cfg.Homeserver, "", "")
	if err != nil {
		return nil, fmt.Errorf("mautrix client: %w", err)
	}

	b := &Bot{client: client, orch: orch, cfg: cfg}
	b.sender = &matrixSender{client: client}
	b.typer = &matrixTyper{client: client}
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
	defer func() { _ = db.Close() }()

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

	if err := b.enforceRoomAllowlist(ctx); err != nil {
		return fmt.Errorf("room allowlist enforcement: %w", err)
	}
	b.registerHandlers()

	if b.OnReady != nil {
		b.OnReady()
	}

	slog.Info("bot: sync starting", "user", b.client.UserID)
	if err := b.client.SyncWithContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}

// enforceRoomAllowlist leaves any joined rooms not on the allowlist.
// Called once at startup before the sync loop begins.
// Returns an error if room membership cannot be verified (fail-closed).
func (b *Bot) enforceRoomAllowlist(ctx context.Context) error {
	resp, err := b.client.JoinedRooms(ctx)
	if err != nil {
		return fmt.Errorf("fetch joined rooms for allowlist enforcement: %w", err)
	}
	var leaveErr error
	for _, roomID := range resp.JoinedRooms {
		if !b.isRoomAllowed(roomID) {
			if _, err := b.client.LeaveRoom(ctx, roomID); err != nil {
				slog.Error("bot: failed to leave disallowed room", "room", roomID, "err", err)
				leaveErr = errors.Join(leaveErr, fmt.Errorf("leave disallowed room %s: %w", roomID, err))
			} else {
				slog.Warn("bot: left disallowed room on startup", "room", roomID)
			}
		}
	}
	if leaveErr != nil {
		return fmt.Errorf("leave disallowed rooms: %w", leaveErr)
	}
	return nil
}

// isRoomAllowed reports whether roomID is on the configured allowlist.
// Returns false when the allowlist is empty or nil (fail-closed).
func (b *Bot) isRoomAllowed(roomID id.RoomID) bool {
	if len(b.cfg.RoomAllowlist) == 0 {
		return false
	}
	_, ok := b.cfg.RoomAllowlist[roomID]
	return ok
}

// registerHandlers wires up event handlers on the syncer.
func (b *Bot) registerHandlers() {
	syncer := b.client.Syncer.(*mautrix.DefaultSyncer)

	// Accept invites only for allowlisted rooms; reject all others.
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		member := evt.Content.AsMember()
		if member == nil {
			return
		}
		if evt.GetStateKey() == b.client.UserID.String() &&
			member.Membership == event.MembershipInvite {
			if !b.isRoomAllowed(evt.RoomID) {
				if _, err := b.client.LeaveRoom(ctx, evt.RoomID); err != nil {
					slog.Error("bot: failed to reject invite from disallowed room", "room", evt.RoomID, "err", err)
				} else {
					slog.Warn("bot: rejected invite from disallowed room", "room", evt.RoomID)
				}
				return
			}
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

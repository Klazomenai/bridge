// Command bridge is the entry point for the AKeyRA crew bridge bot.
// It loads configuration from environment and mounted secrets, initialises
// the crew registry and orchestrator, and starts the Matrix bot.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	ctxbuf "klazomenai/bridge/internal/context"
	"klazomenai/bridge/internal/bot"
	"klazomenai/bridge/internal/crew"
	"klazomenai/bridge/internal/crest"
	"klazomenai/bridge/internal/orchestrator"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// --- Anthropic API key (mounted as file, not env var) ---
	apiKeyBytes, err := os.ReadFile(mustEnv("ANTHROPIC_KEY_PATH", "/run/secrets/anthropic/api_key"))
	if err != nil {
		slog.Error("failed to read Anthropic API key", "err", err)
		os.Exit(1)
	}
	apiKey := strings.TrimSpace(string(apiKeyBytes))
	if apiKey == "" {
		slog.Error("Anthropic API key is empty — check secret mount")
		os.Exit(1)
	}

	// --- Crew registry ---
	registryPath := mustEnv("CREW_REGISTRY_PATH", "/config/crew.yaml")
	registry, err := crew.Load(registryPath)
	if err != nil {
		slog.Error("failed to load crew registry", "err", err)
		os.Exit(1)
	}

	// --- Session context manager ---
	ctxManager := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)

	// --- Orchestrator ---
	orch := orchestrator.New(registry, ctxManager, apiKey)

	// --- Matrix bot ---
	botCfg := bot.Config{
		Homeserver:   mustEnv("MATRIX_HOMESERVER", ""),
		Username:     mustEnv("MATRIX_USERNAME", ""),
		Password:     mustEnv("MATRIX_PASSWORD", ""),
		CryptoDBPath: mustEnv("CRYPTO_DB_PATH", "/data/crypto-store/bridge.db"),
		PickleKey:    mustEnv("PICKLE_KEY", ""),
		DisplayName:  "Bridge",
		KnownCrew:    registry.IDs(),
	}
	if botCfg.Homeserver == "" || botCfg.Username == "" || botCfg.Password == "" || botCfg.PickleKey == "" {
		slog.Error("MATRIX_HOMESERVER, MATRIX_USERNAME, MATRIX_PASSWORD, and PICKLE_KEY are required")
		os.Exit(1)
	}

	matrixBot, err := bot.New(botCfg, orch)
	if err != nil {
		slog.Error("failed to create bot", "err", err)
		os.Exit(1)
	}

	// --- Crest IMAP poller (optional — only started if configured) ---
	imapHost := os.Getenv("CREST_IMAP_HOST")
	if imapHost != "" {
		imapCfg := crest.IMAPConfig{
			Host:     imapHost,
			Port:     1143,
			Username: mustEnv("CREST_IMAP_USERNAME", ""),
			Password: mustEnv("CREST_IMAP_PASSWORD", ""),
			Mailbox:  "INBOX",
		}
		go crest.Poller(ctx, imapCfg, 300*time.Second, func(msgs []crest.Message) {
			slog.Info("crest: received signals", "count", len(msgs))
			// Future: route signals to Crest crew member for processing.
		})
		slog.Info("crest: imap poller started", "host", imapHost)
	}

	// --- Start bot (blocks until ctx cancelled) ---
	slog.Info("bridge: starting")
	if err := matrixBot.Start(ctx); err != nil {
		slog.Error("bot stopped with error", "err", err)
		os.Exit(1)
	}
	slog.Info("bridge: shutdown complete")
}

// mustEnv returns the value of an environment variable, or the fallback if
// the variable is unset. If fallback is "" and the variable is unset, it
// returns "" (caller is responsible for checking).
func mustEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

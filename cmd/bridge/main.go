// Command bridge is the entry point for the AKeyRA crew bridge bot.
// It loads configuration from environment and mounted secrets, initialises
// the crew registry and orchestrator, and starts the Matrix bot.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"klazomenai/bridge/internal/bot"
	ctxbuf "klazomenai/bridge/internal/context"
	"klazomenai/bridge/internal/crest"
	"klazomenai/bridge/internal/crew"
	"klazomenai/bridge/internal/orchestrator"
	"klazomenai/bridge/internal/tools"
	cresttools "klazomenai/bridge/internal/tools/crest"
	marentools "klazomenai/bridge/internal/tools/maren"
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

	// --- Tool registry ---
	toolReg := tools.NewRegistry()
	toolReg.Register(&tools.DelegateTool{})

	// --- Crest email tools ---
	// Registered as stubs when IMAP/SMTP is not configured so crew.yaml
	// validation passes. Claude gets a clear "not configured" error if it
	// tries to use them without the env vars.
	imapHost := os.Getenv("CREST_IMAP_HOST")
	if imapHost != "" {
		imapUser := mustEnv("CREST_IMAP_USERNAME", "")
		imapPass := mustEnv("CREST_IMAP_PASSWORD", "")
		if imapUser == "" || imapPass == "" {
			slog.Error("CREST_IMAP_HOST is set but CREST_IMAP_USERNAME or CREST_IMAP_PASSWORD is missing")
			os.Exit(1)
		}
		imapCfg := crest.IMAPConfig{
			Host:     imapHost,
			Port:     1143,
			Username: imapUser,
			Password: imapPass,
			Mailbox:  "INBOX",
		}
		toolReg.Register(cresttools.NewIMAPPollTool(imapCfg))

		smtpCfg := crest.SMTPConfig{
			Host:     imapHost, // ProtonMail bridge uses same host for IMAP and SMTP
			Port:     1025,
			Username: imapUser,
			Password: imapPass,
			From:     imapUser,
		}
		smtpAllowlist := mustEnv("CREST_SMTP_ALLOWLIST", "")
		if smtpAllowlist == "" {
			slog.Error("CREST_SMTP_ALLOWLIST is required when email tools are configured")
			os.Exit(1)
		}
		toolReg.Register(cresttools.NewSMTPSendTool(smtpCfg, smtpAllowlist))

		slog.Info("crest: email tools registered", "host", imapHost)
	} else {
		toolReg.Register(tools.NewStubTool("imap_poll", "Check email inbox (not configured)"))
		toolReg.Register(tools.NewStubTool("smtp_send", "Send email (not configured)"))
		slog.Info("crest: email tools registered as stubs (CREST_IMAP_HOST not set)")
	}

	// --- Maren / Bosun tools (shared: kubectl_get, helm_status) ---
	if hasExec("kubectl") {
		toolReg.Register(marentools.NewKubectlGetTool(defaultExecFn))
		slog.Info("maren: kubectl_get registered")
	} else {
		toolReg.Register(tools.NewStubTool("kubectl_get", "Get Kubernetes resources (kubectl not available)"))
		slog.Info("maren: kubectl_get registered as stub (binary not found)")
	}
	if hasExec("helm") {
		toolReg.Register(marentools.NewHelmStatusTool(defaultExecFn))
		slog.Info("maren: helm_status registered")
	} else {
		toolReg.Register(tools.NewStubTool("helm_status", "Get Helm release status (helm not available)"))
		slog.Info("maren: helm_status registered as stub (binary not found)")
	}

	// --- Lookout tools ---
	toolReg.Register(tools.NewStubTool("prometheus_query", "Query Prometheus metrics (not configured)"))
	toolReg.Register(tools.NewStubTool("loki_query", "Query Loki logs (not configured)"))
	slog.Info("lookout: tools registered as stubs")

	// --- Chips tools ---
	toolReg.Register(tools.NewStubTool("gh_issue_list", "List GitHub issues (not configured)"))
	toolReg.Register(tools.NewStubTool("gh_issue_view", "View a GitHub issue (not configured)"))
	toolReg.Register(tools.NewStubTool("gh_pr_list", "List GitHub pull requests (not configured)"))
	toolReg.Register(tools.NewStubTool("gh_pr_view", "View a GitHub pull request (not configured)"))
	toolReg.Register(tools.NewStubTool("gh_pr_checks", "Check PR CI status (not configured)"))
	toolReg.Register(tools.NewStubTool("git_log", "View recent git commits (not configured)"))
	toolReg.Register(tools.NewStubTool("git_diff", "View git diff between refs (not configured)"))
	slog.Info("chips: tools registered as stubs")

	// --- Crew registry ---
	registryPath := mustEnv("CREW_REGISTRY_PATH", "/config/crew.yaml")
	registry, err := crew.Load(registryPath)
	if err != nil {
		slog.Error("failed to load crew registry", "err", err)
		os.Exit(1)
	}

	// --- Validate crew tool declarations against registered tools ---
	if err := registry.ValidateTools(toolReg); err != nil {
		slog.Error("crew tool validation failed", "err", err)
		os.Exit(1)
	}

	// --- Session context manager ---
	ctxManager := ctxbuf.NewManager(ctxbuf.DefaultMaxTurns)

	// --- Orchestrator ---
	orch := orchestrator.New(registry, ctxManager, toolReg, apiKey)

	// --- Matrix bot ---
	botCfg := bot.Config{
		Homeserver:   mustEnv("MATRIX_HOMESERVER", ""),
		Username:     mustEnv("MATRIX_USERNAME", ""),
		Password:     mustEnv("MATRIX_PASSWORD", ""),
		CryptoDBPath: mustEnv("CRYPTO_DB_PATH", bot.DefaultCryptoDBPath),
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

	// --- Crest IMAP poller (optional — only started if email tools configured) ---
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

// hasExec reports whether a binary is available on PATH.
func hasExec(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// defaultExecFn is the production ExecFn that runs commands via os/exec.
func defaultExecFn(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

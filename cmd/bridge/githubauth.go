package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
)

// DefaultGitHubTokenPath is the file mount used by the Helm chart in
// `klazomenai/AKeyRA/helm/bridge/templates/deployment.yaml` when
// `chips.enabled` is true. Mirrors the Anthropic-key precedent
// (/run/secrets/anthropic/api_key).
const DefaultGitHubTokenPath = "/run/secrets/github/token"

// LoadGitHubToken returns the Chips GitHub PAT used by the chips
// gh_*/git_* tools to authenticate against the GitHub API. Returns
// the empty string when no token is configured — the caller registers
// stub tools in that case (same behaviour as the pre-#141 env-only
// path).
//
// Default behaviour: reads the token from the file at the path
// GITHUB_TOKEN_FILE (defaulting to DefaultGitHubTokenPath). The
// returned value is whitespace-trimmed — Vault writes the literal
// PAT, but mounted K8s Secrets may carry a trailing newline depending
// on how the secret was created. Trimming makes both shapes produce
// the same token string.
//
// Missing file → empty token → stub tools registered, no error. The
// stub branch is the chart's default state (chips.enabled=false) and
// every consumer of LoadGitHubToken must handle an empty return.
//
// When insecureFromEnv is true (operator passed --insecure-token-
// from-env on the bridge command line), the loader reads from the
// GITHUB_TOKEN env var instead and emits a one-shot slog.Warn line.
// This path is intended for local development only — production
// deployments should leave it false (the default). Env-var-mounted
// secrets are exposed via /proc/<pid>/environ to anyone with read
// access in the container's PID namespace, while a Secret-mounted
// file is read once at startup and never written into the process
// environment.
//
// The token value itself is NEVER logged. Both code paths emit only
// metadata (file path, presence boolean, rune count of the trimmed
// value) so an operator can confirm a token was loaded without the
// value appearing in log infrastructure.
func LoadGitHubToken(insecureFromEnv bool) (string, error) {
	if insecureFromEnv {
		v := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
		slog.Warn(
			"chips: GITHUB_TOKEN loaded from env via --insecure-token-from-env (dev only)",
			"present", v != "",
			"rune_count", len([]rune(v)),
		)
		return v, nil
	}

	path := os.Getenv("GITHUB_TOKEN_FILE")
	if path == "" {
		path = DefaultGitHubTokenPath
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// Default chart state (chips.enabled=false) doesn't mount the
		// Secret, so the file is absent. Caller registers stubs.
		slog.Info("chips: GITHUB_TOKEN_FILE not present; stub tools will register",
			"path", path,
		)
		return "", nil
	}
	if err != nil {
		// Permission denied, IO error, etc. — surface to the caller
		// so main() can decide whether to exit or fall through to
		// stubs. The path is logged but the token contents are not
		// (and aren't available — the read failed).
		return "", fmt.Errorf("read GITHUB_TOKEN_FILE %q: %w", path, err)
	}

	token := strings.TrimSpace(string(raw))
	slog.Info("chips: GITHUB_TOKEN loaded from file",
		"path", path,
		"present", token != "",
		"rune_count", len([]rune(token)),
	)
	return token, nil
}

package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureSlog routes the default slog logger to a buffer for the
// duration of one test. Used to assert that the token loader does
// not leak the raw token value into any log line — the "token never
// logged" half of #141's AC.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	return &buf, func() { slog.SetDefault(original) }
}

func TestLoadGitHubToken_FromFile_CustomPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("ghp_test_token_value\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	t.Setenv("GITHUB_TOKEN_FILE", path)

	tok, err := LoadGitHubToken(false)
	if err != nil {
		t.Fatalf("LoadGitHubToken: %v", err)
	}
	if tok != "ghp_test_token_value" {
		t.Errorf("token = %q, want ghp_test_token_value (trimmed)", tok)
	}
}

func TestLoadGitHubToken_FromFile_TrimsTrailingNewline(t *testing.T) {
	// K8s Secrets mounted via the helm chart can include a trailing
	// newline depending on how the secret was created. The loader
	// strips it so the consumer doesn't pass "ghp_xxx\n" to gh CLI
	// (which rejects tokens with whitespace).
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("ghp_with_newline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN_FILE", path)

	tok, _ := LoadGitHubToken(false)
	if tok != "ghp_with_newline" {
		t.Errorf("trailing newline not trimmed: %q", tok)
	}
}

func TestLoadGitHubToken_FromFile_TrimsLeadingAndTrailingWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("  \tghp_padded\t  \n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN_FILE", path)

	tok, _ := LoadGitHubToken(false)
	if tok != "ghp_padded" {
		t.Errorf("whitespace not trimmed: %q", tok)
	}
}

func TestLoadGitHubToken_FileMissing_ReturnsEmptyNoError(t *testing.T) {
	// Default chart state (chips.enabled=false) doesn't create the
	// Secret, so the file is absent. The loader returns "" + nil so
	// the caller registers stub tools — same shape as pre-#141 when
	// GITHUB_TOKEN env was unset.
	t.Setenv("GITHUB_TOKEN_FILE", "/nonexistent/path/never/exists")

	tok, err := LoadGitHubToken(false)
	if err != nil {
		t.Errorf("missing file should not error, got: %v", err)
	}
	if tok != "" {
		t.Errorf("missing file should return empty, got %q", tok)
	}
}

func TestLoadGitHubToken_FileEmpty_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN_FILE", path)

	tok, err := LoadGitHubToken(false)
	if err != nil {
		t.Errorf("empty file should not error, got: %v", err)
	}
	if tok != "" {
		t.Errorf("empty file should return empty, got %q", tok)
	}
}

func TestLoadGitHubToken_DefaultPathWhenEnvUnset(t *testing.T) {
	// With GITHUB_TOKEN_FILE unset, the loader falls back to
	// DefaultGitHubTokenPath (/run/secrets/github/token). That path
	// won't exist in the test env, so we expect empty+nil (the
	// missing-file branch) — proving the fallback wiring works.
	t.Setenv("GITHUB_TOKEN_FILE", "")

	tok, err := LoadGitHubToken(false)
	if err != nil {
		t.Errorf("default path should not error when missing, got: %v", err)
	}
	if tok != "" {
		t.Errorf("default path missing should return empty, got %q", tok)
	}
}

func TestLoadGitHubToken_InsecureFromEnv_ReadsEnvVar(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_env_only_value")
	// GITHUB_TOKEN_FILE is irrelevant when insecureFromEnv is true.
	t.Setenv("GITHUB_TOKEN_FILE", "/some/path/that/should/be/ignored")

	tok, err := LoadGitHubToken(true)
	if err != nil {
		t.Fatalf("LoadGitHubToken(true): %v", err)
	}
	if tok != "ghp_env_only_value" {
		t.Errorf("env path: token = %q", tok)
	}
}

func TestLoadGitHubToken_InsecureFromEnv_TrimsWhitespace(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "  ghp_env_padded  ")

	tok, _ := LoadGitHubToken(true)
	if tok != "ghp_env_padded" {
		t.Errorf("env whitespace not trimmed: %q", tok)
	}
}

func TestLoadGitHubToken_InsecureFromEnv_EmptyReturnsEmpty(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	tok, err := LoadGitHubToken(true)
	if err != nil {
		t.Errorf("empty env should not error, got: %v", err)
	}
	if tok != "" {
		t.Errorf("empty env should return empty, got %q", tok)
	}
}

func TestLoadGitHubToken_FileTakesPrecedenceOverEnv(t *testing.T) {
	// When insecureFromEnv is FALSE, GITHUB_TOKEN env is completely
	// ignored — only the file path source is consulted. Documents
	// the #142 AC "either deprecated cleanly or kept with documented
	// precedence" decision for the token side: file is the single
	// source of truth in production; env is dev-only via the flag.
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("ghp_from_file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN_FILE", path)
	t.Setenv("GITHUB_TOKEN", "ghp_from_env_should_be_ignored")

	tok, err := LoadGitHubToken(false)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "ghp_from_file" {
		t.Errorf("env shadowed file: %q", tok)
	}
}

func TestLoadGitHubToken_TokenNeverLoggedFromFile(t *testing.T) {
	// #141 AC: "Token is never logged, even in debug mode". Capture
	// every slog line emitted during a load and assert the literal
	// token value does NOT appear. The loader emits a metadata-only
	// info line (path, present bool, rune count); the value itself
	// must not surface anywhere in those structured fields.
	buf, restore := captureSlog(t)
	defer restore()

	const secret = "ghp_DO_NOT_LOG_unique_secret_value_42"
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN_FILE", path)

	if _, err := LoadGitHubToken(false); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(buf.String(), secret) {
		t.Errorf("raw token leaked into slog output: %s", buf.String())
	}
}

func TestLoadGitHubToken_TokenNeverLoggedFromEnv(t *testing.T) {
	// Same contract on the --insecure-token-from-env code path.
	buf, restore := captureSlog(t)
	defer restore()

	const secret = "ghp_env_path_DO_NOT_LOG_value_99"
	t.Setenv("GITHUB_TOKEN", secret)

	if _, err := LoadGitHubToken(true); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(buf.String(), secret) {
		t.Errorf("raw token leaked into slog output (env path): %s", buf.String())
	}
}

func TestLoadGitHubToken_PermissionError_Surfaces(t *testing.T) {
	// Unexpected file-read failures (permission denied, IO error)
	// surface to the caller rather than silently returning empty —
	// otherwise an operator misconfiguration that prevents the
	// bridge from reading a configured secret would silently fall
	// through to stub tools, hiding the real issue.
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission test won't trigger EACCES")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("ghp_unreachable"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN_FILE", path)

	_, err := LoadGitHubToken(false)
	if err == nil {
		t.Error("expected error on unreadable file, got nil")
	}
	// Error message includes the path (for triage) but not the
	// token (we couldn't read it anyway).
	if err != nil && !strings.Contains(err.Error(), path) {
		t.Errorf("error should mention path: %v", err)
	}
}

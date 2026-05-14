// Package chips provides Chips' Carpenter tools: GitHub and git operations.
package chips

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"klazomenai/bridge/internal/tools/redact"
)

// ExecFn executes a command and returns its standard output (stdout).
// Production callers pass a function wrapping os/exec.CommandContext;
// tests inject a mock.
type ExecFn func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultExecFn returns the production exec function using os/exec.
// The child process inherits the parent's os.Environ() and gets no
// extra env vars. Callers that need GITHUB_TOKEN authentication for
// gh-CLI subprocesses should use DefaultExecFnWithToken instead.
func DefaultExecFn() ExecFn {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
}

// DefaultExecFnWithToken returns an ExecFn that injects
// GITHUB_TOKEN into the child process's environment without
// exposing the token in the bridge's own os.Environ(). This is the
// production path when the token is loaded from a mounted secret
// file rather than the bridge process's env (see
// cmd/bridge/githubauth.go's LoadGitHubToken).
//
// Injection is gated on the command name being gh — concretely
// `filepath.Base(name) == "gh"`. The same ExecFn instance is
// passed (via registration.go) to both the gh_* tools and the
// git_log/git_diff tools; only the former invoke the GitHub CLI
// and need the PAT. Narrowing the injection to gh commands keeps
// the token off /proc/<git-pid>/environ for `git log` / `git
// diff` subprocesses (which don't authenticate against the GitHub
// API; git's local operations need no token).
//
// The gh CLI reads GH_TOKEN (preferred) and GITHUB_TOKEN (fallback)
// from its env to authenticate. To make the loaded token
// authoritative — and to guard against a stray GH_TOKEN in the
// bridge's environment silently overriding it because gh prefers
// GH_TOKEN — the inherited env is filtered: every GH_TOKEN= /
// GITHUB_TOKEN= entry from os.Environ() is dropped before we
// append our own GITHUB_TOKEN entry. Other env vars (PATH, HOME,
// etc.) survive into the child unchanged so gh can find its
// dependencies.
//
// Setting GITHUB_TOKEN only on the child cmd's Env keeps the token
// off the bridge process — it doesn't appear in
// /proc/<bridge-pid>/environ, but it DOES appear in
// /proc/<gh-pid>/environ during the brief life of each gh
// subprocess. That window is the minimum any env-passing
// authentication scheme requires.
//
// Passing token="" returns an ExecFn that does NOT add GITHUB_TOKEN
// to the child env (equivalent to DefaultExecFn). This matches the
// existing "empty token → stub tools" gate in main.go: if a
// reduce-confidence caller wires the token through this helper with
// an empty value, the helper degrades to DefaultExecFn rather than
// emitting an invalid `GITHUB_TOKEN=` env entry.
func DefaultExecFnWithToken(token string) ExecFn {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		if env := buildGhChildEnv(os.Environ(), name, token); env != nil {
			cmd.Env = env
		}
		return cmd.Output()
	}
}

// buildGhChildEnv constructs the child process environment for a
// DefaultExecFnWithToken call, or returns nil to signal "let the
// child inherit the parent's os.Environ() unchanged".
//
// Returns nil when token is empty OR when the command's basename
// is anything other than "gh" — both states mean we don't want to
// override Cmd.Env and the caller should leave it as Go's default
// (nil → inherit parent).
//
// When the gate passes, the returned slice is parent's os.Environ()
// with any GH_TOKEN= / GITHUB_TOKEN= entries filtered out, plus a
// fresh GITHUB_TOKEN= entry carrying the injected value at the end.
// Extracted as a pure function so the gating + filtering logic can
// be unit-tested directly without subprocess setup (see
// exec_internal_test.go).
func buildGhChildEnv(parent []string, name, token string) []string {
	if token == "" || filepath.Base(name) != "gh" {
		return nil
	}
	env := make([]string, 0, len(parent)+1)
	for _, e := range parent {
		// Drop both gh-CLI auth env vars so the injected value is
		// the only signal the child sees. Without this filter, a
		// stray GH_TOKEN= in the parent's env would win against
		// our GITHUB_TOKEN= because gh prefers GH_TOKEN.
		if strings.HasPrefix(e, "GH_TOKEN=") || strings.HasPrefix(e, "GITHUB_TOKEN=") {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "GITHUB_TOKEN="+token)
	return env
}

// RepoAllowlist is a set of allowed org/repo pairs.
type RepoAllowlist map[string]bool

// ParseRepoAllowlist parses a comma-separated list of org/repo pairs.
func ParseRepoAllowlist(csv string) RepoAllowlist {
	list := make(RepoAllowlist)
	for _, entry := range strings.Split(csv, ",") {
		entry = strings.TrimSpace(entry)
		if entry != "" {
			list[strings.ToLower(entry)] = true
		}
	}
	return list
}

// Check returns an error if the org/repo is not in the allowlist.
func (a RepoAllowlist) Check(org, repo string) error {
	key := strings.ToLower(org + "/" + repo)
	if !a[key] {
		return fmt.Errorf("repository %q is not in the allowed list", key)
	}
	return nil
}

// SanitiseOutputForTest is an exported alias for testing.
var SanitiseOutputForTest = sanitiseOutput

// sanitiseOutput chains two redaction primitives on every gh_* /
// git_* tool output:
//
//  1. redact.Redact strips the known GITHUB_TOKEN value (substring
//     replacement, callers supply the exact secret).
//  2. Sanitise (this package) applies the redact package default
//     pattern set (obtained via redact.DefaultPatterns) plus any
//     chips-specific extras to catch token-shaped strings in
//     untrusted comment / issue / PR bodies the operator never
//     supplied as a known secret (e.g. an AWS key pasted into a
//     GitHub comment by a third party).
//
// The order is deliberate: known-secret substring replacement is
// cheap and always-correct, so it runs first; pattern-based
// detection is a regex pass under a length cap and runs second.
//
// When tool is non-empty, every pattern match in step 2 emits one
// slog.Info "sanitiser_redaction" line tagged with `tool` (the
// caller's Tool.Name(), e.g. "gh_issue_view") and `field=output`.
// Tests pass tool="" to stay silent; production callers pass
// t.Name().
//
// Scope deviation from #83 AC body: the spec body says "Touch only
// the *content* fields (issue.body, comment.body, review.body),
// never structural fields (numbers, URLs, dates)." This function
// runs the chain over the WHOLE `gh --json` stdout rather than
// unmarshalling and walking specific content-field paths. The
// patterns are narrow enough that numbers, dates, and non-token-
// bearing URLs aren't matched in practice; in the one edge case
// where the deviation matters (a URL query string containing
// `?password=...`), the result is desirable redaction of a
// secret-in-URL leak. Per-field walking was deemed unnecessary
// complexity given the orchestrator-level safety floor in #129
// will apply the same patterns whole-output downstream anyway.
// Pinned in the #83 close-out discussion.
func sanitiseOutput(output, token, tool string) string {
	cleaned := redact.Redact(output, token)
	if tool == "" {
		return Sanitise(cleaned)
	}
	return Sanitise(cleaned,
		slog.String("tool", tool),
		slog.String("field", "output"),
	)
}

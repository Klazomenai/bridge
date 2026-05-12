// Package chips provides Chips' Carpenter tools: GitHub and git operations.
package chips

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"klazomenai/bridge/internal/tools/redact"
)

// ExecFn executes a command and returns its standard output (stdout).
// Production callers pass a function wrapping os/exec.CommandContext;
// tests inject a mock.
type ExecFn func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultExecFn returns the production exec function using os/exec.
func DefaultExecFn() ExecFn {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}
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
//  2. Sanitise (this package) applies the shared redact.Patterns
//     plus any chips-specific extras to catch token-shaped strings
//     in untrusted comment / issue / PR bodies the operator never
//     supplied as a known secret (e.g. an AWS key pasted into a
//     GitHub comment by a third party).
//
// The order is deliberate: known-secret substring replacement is
// cheap and always-correct, so it runs first; pattern-based
// detection is a regex pass under a length cap and runs second.
func sanitiseOutput(output, token string) string {
	return Sanitise(redact.Redact(output, token))
}

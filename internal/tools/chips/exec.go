// Package chips provides Chips' Carpenter tools: GitHub and git operations.
package chips

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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

// sanitiseOutput strips the GITHUB_TOKEN value from output if present.
func sanitiseOutput(output, token string) string {
	if token == "" {
		return output
	}
	return strings.ReplaceAll(output, token, "[REDACTED]")
}

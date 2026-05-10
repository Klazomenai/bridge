// Package redact provides token redaction primitives for tool output
// before it reaches the model, the user, or the audit log.
//
// The package is intentionally minimal: a variadic Redact function that
// substitutes a sentinel for each known secret. Callers are responsible
// for deciding which strings to treat as secrets — this package does
// not detect tokens; it only redacts known ones.
//
// For structured redaction (e.g. kubectl YAML field stripping), see the
// per-package redaction layered on top of Redact (e.g. internal/tools/maren).
package redact

import (
	"sort"
	"strings"
)

// Sentinel is the replacement string substituted for each redacted secret.
const Sentinel = "[REDACTED]"

// Redact returns input with every non-empty secret in secrets replaced
// by Sentinel. Empty secrets are skipped (so callers can pass through
// unset configuration values without conditional logic).
//
// Internally sorts the secrets by descending length before replacement
// so callers do NOT need to reason about overlap ordering. Without
// this guarantee, ["foo", "foobar"] passed in caller-supplied order
// would replace "foo" first and leave "[REDACTED]bar" — a partial
// secret leak. The internal sort ensures the longest match always
// wins regardless of caller order.
func Redact(input string, secrets ...string) string {
	if len(secrets) == 0 {
		return input
	}
	// Defensive copy: filter empties + leave caller's slice untouched.
	filtered := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s != "" {
			filtered = append(filtered, s)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return len(filtered[i]) > len(filtered[j])
	})
	for _, secret := range filtered {
		input = strings.ReplaceAll(input, secret, Sentinel)
	}
	return input
}

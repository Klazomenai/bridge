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

import "strings"

// Sentinel is the replacement string substituted for each redacted secret.
const Sentinel = "[REDACTED]"

// Redact returns input with every non-empty secret in secrets replaced
// by Sentinel. Empty secrets are skipped (so callers can pass through
// unset configuration values without conditional logic).
//
// Redact is a single-pass replacement per secret: if multiple secrets
// share a substring, ordering matters — pass the longer secret first.
func Redact(input string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		input = strings.ReplaceAll(input, secret, Sentinel)
	}
	return input
}

// Package redact provides token redaction primitives for tool output
// before it reaches the model, the user, or the audit log.
//
// Two complementary primitives:
//
//   - Redact substitutes a sentinel for each caller-supplied secret
//     value. The package does not detect tokens; it only redacts known
//     ones. Use this when the secret value is known at the call site
//     (e.g. the GITHUB_TOKEN passed to a tool).
//
//   - Sanitise detects token-shaped strings in untrusted input via a
//     regex pattern set and replaces them with informative sentinels.
//     Use this when the input is attacker-controllable (e.g. GitHub
//     comment bodies, email previews) and the value cannot be known
//     in advance.
//
// For structured redaction (e.g. kubectl YAML field stripping), see the
// per-package redaction layered on top of these primitives (e.g.
// internal/tools/maren).
package redact

import (
	"log/slog"
	"regexp"
	"sort"
	"strings"
)

// Sentinel is the replacement string substituted for each Redacted secret.
const Sentinel = "[REDACTED]"

// MaxSanitiserInputBytes is the upper byte length Sanitise will process.
// Inputs longer than this are truncated to this length before pattern
// matching begins. The cap defends against regex-DoS amplification on
// attacker-controlled bodies (a 1 MB pathological comment costs ~16 ×
// 65 KB regex passes, not ~16 × 1 MB).
//
// Truncation may split a multi-byte UTF-8 rune at the boundary; the
// resulting trailing invalid byte is harmless under Go's regexp (which
// skips invalid UTF-8 in pattern character classes) and is preferred to
// rune-walking for a hot-path defence.
const MaxSanitiserInputBytes = 65536

// SanitiserErrorReplacement is substituted for the entire input when
// Sanitise's internal recover catches a panic. Callers MUST treat this
// as a load-bearing safety floor: Bridge does not pass un-Sanitised
// content to Claude under any circumstances, so a panic-replacement
// surfacing to the model is a far better failure mode than silently
// forwarding raw content.
const SanitiserErrorReplacement = "[SANITISER ERROR — content suppressed]"

// Pattern describes a single Sanitiser rule: a name (for log
// attribution and per-pattern testing), a compiled regex to match,
// and a replacement string substituted for each match.
//
// Replacements should be chosen so that re-applying Sanitise produces
// the same output (idempotence). The default Patterns set below uses
// the U+2026 horizontal ellipsis "…" in replacements to break the
// originating character classes — `ghp_…REDACTED` does not re-match
// `(ghp|gho|...)_[A-Za-z0-9]{36,}` because `…` is multi-byte and
// outside the ASCII letter/digit class.
type Pattern struct {
	Name        string
	Regex       *regexp.Regexp
	Replacement string
}

// Patterns is the shared default Sanitiser pattern set. Order is
// significant only when two patterns could match overlapping input;
// the broader patterns (bearer, password) come last to give the
// narrower token-shape patterns first opportunity.
//
// Adding a pattern here makes it apply to every consumer of
// Sanitise (chips, crest, maren, the orchestrator-level safety floor).
// For per-crew patterns that should NOT apply elsewhere, declare a
// local []Pattern slice in the consumer's package and call
// SanitiseWith.
var Patterns = []Pattern{
	{
		Name:        "aws_access_key",
		Regex:       regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		Replacement: "AKIA…REDACTED",
	},
	{
		Name:        "github_token",
		Regex:       regexp.MustCompile(`(ghp|gho|ghr|ghu|ghs)_[A-Za-z0-9]{36,}`),
		Replacement: "${1}_…REDACTED",
	},
	{
		Name:        "github_pat",
		Regex:       regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`),
		Replacement: "github_pat_…REDACTED",
	},
	{
		Name:        "openai_anthropic_key",
		Regex:       regexp.MustCompile(`sk-(ant-)?[A-Za-z0-9_\-]{20,}`),
		Replacement: "sk-…REDACTED",
	},
	{
		Name:        "slack_token",
		Regex:       regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
		Replacement: "xox-…REDACTED",
	},
	{
		Name:        "jwt",
		Regex:       regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`),
		Replacement: "JWT-REDACTED",
	},
	{
		Name:        "pem_block",
		Regex:       regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+ KEY-----.*?-----END [A-Z ]+ KEY-----`),
		Replacement: "-----BEGIN … KEY----- REDACTED -----END … KEY-----",
	},
	{
		Name: "bearer_token",
		// {16,} avoids false-positives on English usage of the word
		// bearer ("the bearer of bad news", "bearer bond holder")
		// while still catching realistic OAuth / API bearer tokens,
		// which are universally 20+ chars (JWTs are 100+).
		Regex:       regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{16,}`),
		Replacement: "Bearer REDACTED",
	},
	{
		Name:        "password_assignment",
		Regex:       regexp.MustCompile(`(?i)password\s*[:=]\s*\S+`),
		Replacement: "password: REDACTED",
	},
}

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

// Sanitise runs the shared Patterns set over input. Inputs longer
// than MaxSanitiserInputBytes are truncated before pattern matching.
// See SanitiseWith for the failure-mode contract.
func Sanitise(input string) string {
	return SanitiseWith(input, Patterns)
}

// SanitiseWith runs the supplied pattern set over input with the same
// length-ceiling and fail-closed guarantees as Sanitise.
//
// Fail-closed: any panic during pattern application is recovered, the
// entire return value is replaced with SanitiserErrorReplacement, and
// an slog.Error line is emitted with the panic value and the input
// byte length (NOT the input itself — leaking a suspected-toxic
// payload into logs would defeat the redaction).
func SanitiseWith(input string, patterns []Pattern) (out string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("sanitiser panic recovered",
				"panic", r,
				"input_bytes", len(input),
			)
			out = SanitiserErrorReplacement
		}
	}()

	// Truncate before any pattern matching — guards against
	// regex-DoS on attacker-controlled inputs.
	if len(input) > MaxSanitiserInputBytes {
		input = input[:MaxSanitiserInputBytes]
	}

	out = input
	for _, p := range patterns {
		out = p.Regex.ReplaceAllString(out, p.Replacement)
	}
	return out
}

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

// Sentinel is the replacement string substituted for each redacted secret.
const Sentinel = "[REDACTED]"

// MaxSanitiserInputBytes is the upper byte length Sanitise will process
// (64 KiB exactly = 65 536 bytes). Inputs longer than this are
// truncated to this length before pattern matching begins. The cap
// defends against regex-DoS amplification on attacker-controlled
// bodies: pattern-matching cost is bounded to N × 64 KiB regex
// passes (N = number of patterns in the default set) rather than
// N × original-input-size, regardless of how many patterns are added
// later.
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

// defaultPatterns is the shared default Sanitiser pattern set,
// unexported so external consumers cannot mutate it (append, reorder,
// modify) and race with concurrent Sanitise calls. Use DefaultPatterns
// to obtain a defensive copy.
//
// Order is significant only when two patterns could match overlapping
// input; the broader patterns (bearer, password) come last to give
// the narrower token-shape patterns first opportunity.
//
// Adding a pattern here makes it apply to every consumer of Sanitise
// (chips, crest, maren, the orchestrator-level safety floor). For
// per-crew patterns that should NOT apply elsewhere, declare a local
// []Pattern slice in the consumer's package and call SanitiseWith.
var defaultPatterns = []Pattern{
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
		Name: "slack_token",
		// Capture the type char (b/a/p/r/s) so the sentinel preserves
		// which Slack token shape was redacted — useful when reading
		// sanitised output for incident debugging (xoxb-…REDACTED is a
		// bot token; xoxp-…REDACTED is a user token; etc.).
		Regex:       regexp.MustCompile(`xox([baprs])-[A-Za-z0-9-]{10,}`),
		Replacement: "xox${1}-…REDACTED",
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
		Name: "password_assignment",
		// Capture the matched keyword + delimiter so original case
		// and the chosen delimiter survive the redaction — env-var
		// style (DATABASE_PASSWORD=...) stays readable as
		// DATABASE_PASSWORD=REDACTED rather than being mangled to
		// DATABASE_password: REDACTED. Case-insensitive match
		// preserves the operator's writing convention.
		Regex:       regexp.MustCompile(`(?i)(password\s*[:=]\s*)\S+`),
		Replacement: "${1}REDACTED",
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

// Sanitise runs the package default pattern set over input. Inputs
// longer than MaxSanitiserInputBytes are truncated before pattern
// matching. See SanitiseWith for the failure-mode contract.
func Sanitise(input string) string {
	return SanitiseWith(input, defaultPatterns)
}

// DefaultPatterns returns a defensive copy of the shared default
// pattern set applied by Sanitise. Use this when composing the
// default set with per-crew extras (see chips.Sanitise for the
// canonical pattern) so the underlying slice cannot be mutated by
// downstream consumers and race with concurrent Sanitise calls.
//
// Each call returns an independent backing array — separate
// DefaultPatterns calls do NOT share slice storage with each other
// or with the unexported defaultPatterns. The returned slice is
// safe to mutate at the slice level (append, reorder, drop, or
// replace whole Pattern values) without affecting any other caller.
//
// CAVEAT — pointer sharing on Pattern.Regex: the Regex field is a
// *regexp.Regexp pointer that IS shared with the unexported
// defaultPatterns (only the Pattern struct values are copied, not
// the regex instances). Do NOT call mutating methods on a returned
// Pattern's Regex (notably Regexp.Longest, which changes match mode)
// — that change races with concurrent Sanitise calls and silently
// alters every other consumer's matching behaviour. To substitute
// a pattern, REPLACE the Pattern struct in the slice
// (mySlice[i] = Pattern{Name: ..., Regex: regexp.MustCompile(...),
// Replacement: ...}) rather than mutating the existing pattern's
// fields in place.
//
// The hazard to avoid at the slice level is sharing a single
// mutated slice instance across consumers (e.g. caching one
// DefaultPatterns return in a package-level var and letting two
// components append to it from different code paths). If a consumer
// intends to mutate, it should call DefaultPatterns afresh for its
// own copy.
func DefaultPatterns() []Pattern {
	out := make([]Pattern, len(defaultPatterns))
	copy(out, defaultPatterns)
	return out
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
	// Capture the original length BEFORE truncation so a deferred
	// panic log reflects the actual payload size that crashed the
	// sanitiser, not the post-truncation slice length. Useful for
	// incident triage ("how big was the toxic payload?") and lost
	// otherwise because `input` is reassigned below.
	origLen := len(input)

	defer func() {
		if r := recover(); r != nil {
			slog.Error("sanitiser panic recovered",
				"panic", r,
				"input_bytes", origLen,
				"truncated", origLen > MaxSanitiserInputBytes,
			)
			out = SanitiserErrorReplacement
		}
	}()

	// Truncate INPUT before any pattern matching — guards against
	// regex-DoS on attacker-controlled inputs. strings.Clone is
	// load-bearing on the memory side of the cap: a bare
	// `input[:Max]` slice retains the full original backing
	// allocation via the string header's data pointer, so a 1 MiB
	// attacker payload would stay alive in memory for as long as
	// the truncated string did. Clone forces a fresh 64 KiB
	// allocation and lets the original be GC'd.
	if origLen > MaxSanitiserInputBytes {
		input = strings.Clone(input[:MaxSanitiserInputBytes])
	}

	out = input
	for _, p := range patterns {
		out = p.Regex.ReplaceAllString(out, p.Replacement)
	}

	// Truncate OUTPUT to the same cap. Some pattern replacements run
	// slightly longer than their minimal input match — the U+2026
	// ellipsis is 3 UTF-8 bytes where the matched byte was 1 ASCII —
	// so an adversarial body packed with minimal-length matches (e.g.
	// 4096 `xoxb-1234567890 ` units = exactly 65 536 bytes) would
	// produce output of 69 632 bytes without this cap. Re-truncating
	// preserves the byte-budget invariant for downstream consumers
	// (notably the orchestrator-level safety floor in #129, which
	// chains another Sanitise pass and benefits from a known upper
	// bound on its own input size). Clone for the same memory-defence
	// reason as the input path: a pre-truncation `out` longer than
	// the cap stays alive via the truncated slice's backing pointer
	// otherwise.
	if len(out) > MaxSanitiserInputBytes {
		out = strings.Clone(out[:MaxSanitiserInputBytes])
	}
	return out
}

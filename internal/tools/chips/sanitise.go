package chips

import (
	"log/slog"

	"klazomenai/bridge/internal/tools/redact"
)

// chipsPatterns holds Chips-specific Sanitiser rules that supplement
// the shared redact default pattern set (obtained via
// redact.DefaultPatterns at composition time). Empty today — the
// shared set covers Chips' threat model (untrusted GitHub comment /
// issue / PR bodies containing token-shaped strings). Reserved as an
// extension point if a GitHub-specific shape arises that should NOT
// apply to other crew (e.g. GitHub repo deploy keys with a shape
// distinct from any other secret).
var chipsPatterns = []redact.Pattern{}

// allChipsPatterns returns the combined Sanitiser pattern set used by
// Sanitise: the shared redact default patterns followed by
// chipsPatterns. Constructed per-call via redact.DefaultPatterns so
// chips cannot accidentally mutate the package default set, and so
// tests reordering or shadowing the underlying slices do not produce
// stale composite state.
func allChipsPatterns() []redact.Pattern {
	return append(redact.DefaultPatterns(), chipsPatterns...)
}

// Sanitise applies the shared redact patterns plus any Chips-specific
// additions to input. Fail-closed: any internal panic returns
// redact.SanitiserErrorReplacement (see redact.SanitiseWith).
//
// Optional logAttrs are passed through to redact.SanitiseWith and
// emit one slog.Info line per matched pattern (with pattern_name +
// count). Callers in the gh_* / git_* tool layer (see sanitiseOutput
// in exec.go) pass `tool` and `field` attrs; tests and the
// orchestrator-level safety floor in #129 pass none and stay silent.
//
// This is the per-tool first line of defence applied at every gh_*
// tool's output boundary. The orchestrator-level safety floor (issue
// #129) applies the same shared patterns again to every tool_result
// regardless of which tool produced it.
func Sanitise(input string, logAttrs ...slog.Attr) string {
	return redact.SanitiseWith(input, allChipsPatterns(), logAttrs...)
}

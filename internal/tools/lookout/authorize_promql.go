package lookout

import (
	"fmt"
	"regexp"
	"regexp/syntax"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// namespaceLabel is the Kubernetes standard label name Prometheus series carry
// when scraped via the kubernetes_sd_configs in the kube-prometheus-stack.
// Lookout's allowlist enforcement requires every vector selector to constrain
// this label.
const namespaceLabel = "namespace"

// promParser is a single shared Parser instance — Prometheus' ParseExpr is
// safe to call concurrently on one parser because each call allocates its own
// internal state.
var promParser = parser.NewParser(parser.Options{})

// AuthorizePromQL parses a PromQL query and rejects it unless every vector
// selector in the AST constrains the "namespace" label to a value (or set of
// values) drawn entirely from the allowlist.
//
// Rejection cases:
//   - Parse error (malformed PromQL).
//   - Any vector selector lacks a "namespace" label matcher.
//   - A "namespace" matcher uses MatchNotEqual or MatchNotRegexp (negative
//     matchers are exclusion, not inclusion — they grant visibility to
//     everything except the named value).
//   - A "namespace" matcher uses MatchEqual but the value is not in the
//     allowlist.
//   - A "namespace" matcher uses MatchRegexp and the regex is not a literal
//     anchored alternation of allowlisted values (e.g. "^(foo|bar)$").
//     Arbitrary regex (including ".*", ".+", "ma.*", "matrix|.*") is rejected.
//
// Returns nil if every vector selector in the AST is authorised.
func AuthorizePromQL(query string, allowlist *NamespaceAllowlist) error {
	if allowlist == nil || allowlist.Len() == 0 {
		return fmt.Errorf("lookout: namespace allowlist is empty — query refused")
	}

	expr, err := promParser.ParseExpr(query)
	if err != nil {
		return fmt.Errorf("lookout: invalid PromQL: %w", err)
	}

	var authErr error
	parser.Inspect(expr, func(node parser.Node, _ []parser.Node) error {
		vs, ok := node.(*parser.VectorSelector)
		if !ok {
			return nil
		}
		if err := checkSelector(vs.LabelMatchers, allowlist); err != nil {
			authErr = err
			return err // stops further traversal
		}
		return nil
	})
	return authErr
}

// checkSelector verifies a set of label matchers satisfies the namespace
// constraint.
func checkSelector(matchers []*labels.Matcher, allowlist *NamespaceAllowlist) error {
	var ns *labels.Matcher
	for _, m := range matchers {
		if m.Name == namespaceLabel {
			if ns != nil {
				// PromQL disallows duplicate label matchers at parse time, but
				// guard defensively against upstream changes.
				return fmt.Errorf("lookout: duplicate namespace matcher")
			}
			ns = m
		}
	}
	if ns == nil {
		return fmt.Errorf("lookout: query missing required namespace matcher (allowed namespaces: %v)", allowlist.Names())
	}
	return checkNamespaceMatcher(ns, allowlist)
}

// checkNamespaceMatcher verifies a single namespace matcher is safe.
func checkNamespaceMatcher(m *labels.Matcher, allowlist *NamespaceAllowlist) error {
	switch m.Type {
	case labels.MatchEqual:
		if !allowlist.Contains(m.Value) {
			return fmt.Errorf("lookout: namespace %q not in allowlist %v", m.Value, allowlist.Names())
		}
		return nil

	case labels.MatchRegexp:
		return checkNamespaceRegex(m.Value, allowlist)

	case labels.MatchNotEqual, labels.MatchNotRegexp:
		return fmt.Errorf("lookout: negative namespace matcher %q not permitted", m.String())
	}
	return fmt.Errorf("lookout: unsupported namespace matcher type %v", m.Type)
}

// flagDirectiveRe catches every Perl-style inline flag directive at the
// pattern-string level — `(?i)`, `(?m)`, `(?-i)`, `(?i:...)`, `(?im:...)`,
// `(?U)`, etc. — without having to inspect the parsed AST. This is important
// because some directives SET flag bits (e.g. `(?i)` sets FoldCase) while
// others CLEAR them (e.g. `(?m)` clears OneLine), so a bit-mask check on
// syntax.Regexp.Flags cannot detect all of them uniformly.
//
// The pattern matches `(?` followed by one or more flag letters (or a `-`
// for "unset flag") terminated by `:` or `)`. It does NOT match:
//   - `(?:...)` — non-capturing group (no flag letters)
//   - `(?P<name>...)` — Go's named-capture syntax (after `P` is `<` not `:`/`)`)
//   - an escaped literal `\(\?` — the backslash separates `(` from `?`
var flagDirectiveRe = regexp.MustCompile(`\(\?[a-zA-Z\-]+[:)]`)

// unsafeRegexFlags are inline flag directives that SET a bit the default Perl
// parser state doesn't carry. Retained as a belt-and-braces check after the
// string-level rejection above. Note: `(?m)` (clears OneLine) cannot be
// caught by this mask alone — it removes a bit rather than adding one. See
// flagDirectiveRe.
const unsafeRegexFlags = syntax.FoldCase | syntax.DotNL | syntax.NonGreedy | syntax.Literal

// checkNamespaceRegex verifies the regex is a literal anchored alternation of
// allowlisted values with no user-supplied inline flag directives.
//
// Prometheus implicitly anchors all label regex matchers with ^(?: ... )$,
// matching the whole label value. We parse the user-supplied regex, require
// it to be a simple alternation (or a single literal) with no non-default
// flags, and verify every alternative is a plain string in the allowlist.
//
// Rejected:
//   - ".*", ".+", "^.*$"         (wildcards — unbounded)
//   - "ma.*", "ma.+"             (partial wildcards)
//   - "matrix|.*"                (wildcard alternative)
//   - "[a-z]+"                   (character classes)
//   - "(?i)matrix"               (case-insensitive flag — the upstream parser
//                                 normalises runes to upper-case when FoldCase
//                                 is set, so literal extraction would miss the
//                                 widened match at the backend)
//   - "(?i:foo)bar"              (flag on a sub-group)
//   - "matrix(?i)argocd"         (flag mid-expression)
//   - ".{1,10}"                  (repetition)
//
// Accepted:
//   - "matrix"                   (single literal)
//   - "matrix|argocd"            (literal alternation, all entries allowlisted)
//   - "(matrix|argocd)"          (parenthesised alternation)
func checkNamespaceRegex(pattern string, allowlist *NamespaceAllowlist) error {
	if flagDirectiveRe.MatchString(pattern) {
		return fmt.Errorf("lookout: inline regex flag directive not permitted in %q", pattern)
	}
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return fmt.Errorf("lookout: invalid namespace regex %q: %w", pattern, err)
	}
	if err := checkNoUnsafeFlags(re); err != nil {
		return fmt.Errorf("lookout: namespace regex %q rejected: %w", pattern, err)
	}
	values, err := extractLiterals(re)
	if err != nil {
		return fmt.Errorf("lookout: namespace regex %q rejected: %w", pattern, err)
	}
	for _, v := range values {
		if !allowlist.Contains(v) {
			return fmt.Errorf("lookout: namespace %q (from regex %q) not in allowlist %v", v, pattern, allowlist.Names())
		}
	}
	return nil
}

// checkNoUnsafeFlags walks a parsed regex tree and returns an error if any
// node — root, intermediate group, or leaf literal — carries a user-controlled
// inline flag from unsafeRegexFlags.
//
// Go's regexp/syntax parser propagates inline-flag directives to the AST
// nodes they apply to. For a literal the flag can appear on the leaf (e.g.
// `matrix(?i)argocd` tags only the "ARGOCD" OpLiteral, NOT the parent
// OpConcat). A top-level-only check would therefore miss mid-expression
// flag directives; we must recurse.
func checkNoUnsafeFlags(re *syntax.Regexp) error {
	if re.Flags&unsafeRegexFlags != 0 {
		return fmt.Errorf("inline flag directive not permitted (flags 0x%x)", re.Flags&unsafeRegexFlags)
	}
	for _, sub := range re.Sub {
		if err := checkNoUnsafeFlags(sub); err != nil {
			return err
		}
	}
	return nil
}

// extractLiterals walks a regex AST and returns the set of literal strings it
// can match. Returns an error if the regex is not a literal alternation (e.g.,
// contains wildcards, character classes, repetition).
func extractLiterals(re *syntax.Regexp) ([]string, error) {
	// Prometheus anchors regex matchers at both ends. The parser returns a
	// tree that may be wrapped in OpCapture; unwrap it.
	re = unwrap(re)

	switch re.Op {
	case syntax.OpLiteral:
		return []string{string(re.Rune)}, nil

	case syntax.OpAlternate:
		var out []string
		for _, sub := range re.Sub {
			lits, err := extractLiterals(sub)
			if err != nil {
				return nil, err
			}
			out = append(out, lits...)
		}
		return out, nil

	case syntax.OpEmptyMatch:
		// An empty alternative (e.g., "a||b") would match the empty namespace
		// label, which no real series will carry. Reject rather than accept.
		return nil, fmt.Errorf("empty alternative is not a valid namespace")

	case syntax.OpConcat:
		// A concat of literals is still a literal (e.g., "ma" "trix" → "matrix").
		// Walk sub-expressions; if they're all literals or empty-match, join them.
		var b []rune
		for _, sub := range re.Sub {
			sub = unwrap(sub)
			switch sub.Op {
			case syntax.OpLiteral:
				b = append(b, sub.Rune...)
			case syntax.OpEmptyMatch:
				// skip
			default:
				return nil, fmt.Errorf("unsupported regex construct in concat: %s", sub.Op)
			}
		}
		if len(b) == 0 {
			return nil, fmt.Errorf("empty concat is not a valid namespace")
		}
		return []string{string(b)}, nil

	default:
		return nil, fmt.Errorf("unsupported regex construct %s (only literal alternations permitted)", re.Op)
	}
}

// unwrap removes OpCapture wrappers that regexp/syntax inserts around
// parenthesised groups. The parse tree is otherwise unchanged.
func unwrap(re *syntax.Regexp) *syntax.Regexp {
	for re.Op == syntax.OpCapture && len(re.Sub) == 1 {
		re = re.Sub[0]
	}
	return re
}

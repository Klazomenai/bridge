package lookout_test

import (
	"strings"
	"testing"

	"klazomenai/bridge/internal/tools/lookout"
)

func promAllowlist() *lookout.NamespaceAllowlist {
	return lookout.NewNamespaceAllowlist([]string{"matrix", "argocd", "monitoring"})
}

// --- happy paths ---

func TestAuthorizePromQL_SimpleEquality(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace="matrix"}`, promAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizePromQL_EqualityWithOtherLabels(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace="matrix",job="node",instance="x:9100"}`, promAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizePromQL_RegexLiteralSingle(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"matrix"}`, promAllowlist()); err != nil {
		t.Errorf("expected allow for regex-literal, got: %v", err)
	}
}

func TestAuthorizePromQL_RegexAlternationAllAllowed(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"matrix|argocd"}`, promAllowlist()); err != nil {
		t.Errorf("expected allow for allowlisted alternation, got: %v", err)
	}
}

func TestAuthorizePromQL_RegexParenthesisedAlternation(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"(matrix|argocd)"}`, promAllowlist()); err != nil {
		t.Errorf("expected allow for parenthesised alternation, got: %v", err)
	}
}

func TestAuthorizePromQL_BinaryExprBothAllowed(t *testing.T) {
	q := `sum(up{namespace="matrix"}) + sum(up{namespace="argocd"})`
	if err := lookout.AuthorizePromQL(q, promAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizePromQL_RangeVectorFunction(t *testing.T) {
	q := `rate(http_requests_total{namespace="matrix"}[5m])`
	if err := lookout.AuthorizePromQL(q, promAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizePromQL_AggregateByAllowed(t *testing.T) {
	q := `sum(rate(http_requests_total{namespace="matrix"}[5m])) by (pod)`
	if err := lookout.AuthorizePromQL(q, promAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizePromQL_AggregateWithoutNamespaceAllowed(t *testing.T) {
	// `without (namespace)` drops the label from the RESULT, but every vector
	// selector in the query still filters by an allowlisted namespace, so the
	// data read is still scoped. Allow.
	q := `sum(up{namespace="matrix"}) without (namespace)`
	if err := lookout.AuthorizePromQL(q, promAllowlist()); err != nil {
		t.Errorf("expected allow (vector selectors are scoped), got: %v", err)
	}
}

// --- rejection: missing namespace ---

func TestAuthorizePromQL_NakedVector(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up`, promAllowlist()); err == nil {
		t.Error("expected rejection for naked vector")
	} else if !strings.Contains(err.Error(), "missing required namespace matcher") {
		t.Errorf("expected namespace-missing error, got: %v", err)
	}
}

func TestAuthorizePromQL_MetricWithoutNamespace(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{job="node"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for vector without namespace matcher")
	}
}

func TestAuthorizePromQL_NameSelector(t *testing.T) {
	// {__name__="up"} is equivalent to `up` but via the special name matcher.
	// It still has no namespace selector.
	if err := lookout.AuthorizePromQL(`{__name__="up"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for __name__-only selector")
	}
}

func TestAuthorizePromQL_BinaryExprOneNaked(t *testing.T) {
	// First term has namespace, second is naked. AST walk must reject on the
	// second, not short-circuit on the first.
	q := `sum(up{namespace="matrix"}) + sum(up)`
	if err := lookout.AuthorizePromQL(q, promAllowlist()); err == nil {
		t.Error("expected rejection when any selector is unscoped")
	}
}

// --- rejection: wildcard escapes ---

func TestAuthorizePromQL_RegexWildcardDotStar(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~".*"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for .*")
	}
}

func TestAuthorizePromQL_RegexWildcardDotPlus(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~".+"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for .+")
	}
}

func TestAuthorizePromQL_RegexPartialWildcard(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"ma.*"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for partial wildcard matching matrix and more")
	}
}

func TestAuthorizePromQL_RegexAlternationWithWildcard(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"matrix|.*"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for alternation containing wildcard")
	}
}

func TestAuthorizePromQL_RegexCharacterClass(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"[a-z]+"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for character class")
	}
}

func TestAuthorizePromQL_RegexRepetition(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"matrix{1,10}"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for repetition")
	}
}

func TestAuthorizePromQL_RegexWithFlags(t *testing.T) {
	// Case-insensitive flag could let "MATRIX" or "Matrix" through if the
	// backend accepted them.
	if err := lookout.AuthorizePromQL(`up{namespace=~"(?i)matrix"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for regex with flags")
	}
}

// TestAuthorizePromQL_RegexFoldCaseBypassAttempt defends against the subtle
// case-fold bypass Copilot flagged on PR #108: (?i)matrix parses to an
// OpLiteral with Rune="MATRIX" and FoldCase=true. A naive literal-equality
// check against the allowlist accidentally rejects this because "MATRIX" is
// not in a typical lowercase allowlist — but if an uppercase value IS in the
// allowlist, the fold-case widens the backend match to ALL case variants.
// The explicit flag check protects against this regardless of allowlist
// contents.
func TestAuthorizePromQL_RegexFoldCaseBypassAttempt(t *testing.T) {
	upperAllow := lookout.NewNamespaceAllowlist([]string{"MATRIX"})
	err := lookout.AuthorizePromQL(`up{namespace=~"(?i)matrix"}`, upperAllow)
	if err == nil {
		t.Error("expected rejection for case-fold flag even when uppercase value is allowlisted")
	}
}

// TestAuthorizePromQL_RegexFlagOnSubgroup probes `(?i:foo)bar` — the flag
// applies only to the sub-group. Go's parser puts FoldCase on the leaf
// OpLiteral "FOO" but leaves the sibling "bar" clean. The recursive walker
// must still reject.
func TestAuthorizePromQL_RegexFlagOnSubgroup(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"(?i:matrix)argocd"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for flag on sub-group")
	}
}

// TestAuthorizePromQL_RegexFlagMidExpression probes `matrix(?i)argocd`
// where the flag only tags later literals. The parent OpConcat has clean
// flags; only the second leaf OpLiteral is tagged. The recursive walker
// must still reject.
func TestAuthorizePromQL_RegexFlagMidExpression(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"matrix(?i)argocd"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for mid-expression flag directive")
	}
}

// TestAuthorizePromQL_RegexDotNLFlag probes `(?s)` — makes `.` match
// newlines. Irrelevant for literals but the flag check rejects it pre-emptively.
func TestAuthorizePromQL_RegexDotNLFlag(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace=~"(?s)matrix"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for (?s) flag")
	}
}

// --- rejection: non-allowlisted value ---

func TestAuthorizePromQL_EqualityNotInAllowlist(t *testing.T) {
	err := lookout.AuthorizePromQL(`up{namespace="kube-system"}`, promAllowlist())
	if err == nil {
		t.Error("expected rejection for kube-system")
	}
	if !strings.Contains(err.Error(), "kube-system") {
		t.Errorf("expected rejected namespace in error, got: %v", err)
	}
}

func TestAuthorizePromQL_RegexAlternationMixed(t *testing.T) {
	err := lookout.AuthorizePromQL(`up{namespace=~"matrix|kube-system"}`, promAllowlist())
	if err == nil {
		t.Error("expected rejection when any alternative is not allowlisted")
	}
}

// --- rejection: negative matchers ---

func TestAuthorizePromQL_NotEqual(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace!="kube-system"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for != matcher (grants everything but named value)")
	}
}

func TestAuthorizePromQL_NotRegexp(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace!~"kube-.*"}`, promAllowlist()); err == nil {
		t.Error("expected rejection for !~ matcher")
	}
}

// --- rejection: parse errors ---

func TestAuthorizePromQL_GarbledQuery(t *testing.T) {
	if err := lookout.AuthorizePromQL(`sum(((`, promAllowlist()); err == nil {
		t.Error("expected rejection for unbalanced parens")
	}
}

func TestAuthorizePromQL_EmptyQuery(t *testing.T) {
	if err := lookout.AuthorizePromQL(``, promAllowlist()); err == nil {
		t.Error("expected rejection for empty query")
	}
}

// --- rejection: empty allowlist is fail-closed ---

func TestAuthorizePromQL_EmptyAllowlistDeniesEverything(t *testing.T) {
	empty := lookout.NewNamespaceAllowlist(nil)
	if err := lookout.AuthorizePromQL(`up{namespace="matrix"}`, empty); err == nil {
		t.Error("expected rejection on empty allowlist (fail-closed)")
	}
}

func TestAuthorizePromQL_NilAllowlistDeniesEverything(t *testing.T) {
	if err := lookout.AuthorizePromQL(`up{namespace="matrix"}`, nil); err == nil {
		t.Error("expected rejection on nil allowlist (fail-closed)")
	}
}

// --- subquery and complex expressions ---

func TestAuthorizePromQL_Subquery(t *testing.T) {
	q := `max_over_time(rate(http_requests_total{namespace="matrix"}[1m])[5m:30s])`
	if err := lookout.AuthorizePromQL(q, promAllowlist()); err != nil {
		t.Errorf("expected allow for scoped subquery, got: %v", err)
	}
}

func TestAuthorizePromQL_SubqueryNaked(t *testing.T) {
	q := `max_over_time(rate(http_requests_total[1m])[5m:30s])`
	if err := lookout.AuthorizePromQL(q, promAllowlist()); err == nil {
		t.Error("expected rejection for naked selector inside subquery")
	}
}

func TestAuthorizePromQL_NestedAggregation(t *testing.T) {
	q := `sum by (job) (avg by (pod,job) (up{namespace="matrix"}))`
	if err := lookout.AuthorizePromQL(q, promAllowlist()); err != nil {
		t.Errorf("expected allow for nested aggregation, got: %v", err)
	}
}

package lookout_test

import (
	"strings"
	"testing"

	"klazomenai/bridge/internal/tools/lookout"
)

func logAllowlist() *lookout.NamespaceAllowlist {
	return lookout.NewNamespaceAllowlist([]string{"matrix", "argocd", "monitoring"})
}

// --- happy paths ---

func TestAuthorizeLogQL_StreamSelectorSimple(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{namespace="matrix"}`, logAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizeLogQL_StreamSelectorWithLineFilter(t *testing.T) {
	q := `{namespace="matrix"} |= "error"`
	if err := lookout.AuthorizeLogQL(q, logAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizeLogQL_StreamSelectorWithJSONParser(t *testing.T) {
	q := `{namespace="matrix"} | json | level="error"`
	if err := lookout.AuthorizeLogQL(q, logAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizeLogQL_MetricQuery(t *testing.T) {
	q := `rate({namespace="matrix"} |= "error" [5m])`
	if err := lookout.AuthorizeLogQL(q, logAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizeLogQL_AggregatedMetricQuery(t *testing.T) {
	q := `sum by (app) (rate({namespace="matrix"} |= "error" [5m]))`
	if err := lookout.AuthorizeLogQL(q, logAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizeLogQL_AdditionalLabelsBeyondNamespace(t *testing.T) {
	q := `{namespace="matrix", app="bridge", level="error"}`
	if err := lookout.AuthorizeLogQL(q, logAllowlist()); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestAuthorizeLogQL_RegexEqualityAllAllowed(t *testing.T) {
	q := `{namespace=~"matrix|argocd"}`
	if err := lookout.AuthorizeLogQL(q, logAllowlist()); err != nil {
		t.Errorf("expected allow for allowlisted alternation, got: %v", err)
	}
}

func TestAuthorizeLogQL_MultipleStreamsBothAllowed(t *testing.T) {
	// A binary op between two range vectors, each with its own stream selector.
	// Both must be authorised.
	q := `sum(rate({namespace="matrix"}[5m])) / sum(rate({namespace="argocd"}[5m]))`
	if err := lookout.AuthorizeLogQL(q, logAllowlist()); err != nil {
		t.Errorf("expected allow for both allowlisted streams, got: %v", err)
	}
}

func TestAuthorizeLogQL_RegexInLineFilterIgnored(t *testing.T) {
	// Line-filter regex is a log-content regex, not a label matcher. It is not
	// subject to the namespace rule, and must not confuse the extractor even
	// when it contains "{...}" repetitions.
	q := `{namespace="matrix"} |~ "retry_count=[0-9]{1,3}"`
	if err := lookout.AuthorizeLogQL(q, logAllowlist()); err != nil {
		t.Errorf("expected allow (braces inside string are ignored), got: %v", err)
	}
}

func TestAuthorizeLogQL_RegexInMatcherValueWithBraces(t *testing.T) {
	// Namespace regex matcher containing `{1}` inside its quoted value. The
	// string-aware extractor must treat the inner `{` as part of the string,
	// not as a new selector boundary.
	q := `{namespace=~"matrix{1}"}`
	// Still must be a literal-only regex — "matrix{1}" uses repetition so it
	// will be rejected by the promql regex-literal check. Verify the braces
	// don't confuse the *extractor* (i.e., we should see the rejection come
	// from the authorisation layer, not a parse error at the extractor).
	err := lookout.AuthorizeLogQL(q, logAllowlist())
	if err == nil {
		t.Error("expected rejection for repetition inside namespace regex")
	}
	// The error should NOT be about unbalanced braces — that would indicate
	// the extractor lost track.
	if strings.Contains(err.Error(), "unbalanced") || strings.Contains(err.Error(), "unterminated") {
		t.Errorf("extractor miscounted braces inside quoted value: %v", err)
	}
}

// --- rejection: missing namespace ---

func TestAuthorizeLogQL_NoStreamSelector(t *testing.T) {
	// LogQL requires a stream selector; verify our own check fires before
	// upstream parsing errors.
	if err := lookout.AuthorizeLogQL(`rate([5m])`, logAllowlist()); err == nil {
		t.Error("expected rejection for query with no stream selector")
	}
}

func TestAuthorizeLogQL_SelectorWithoutNamespace(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{app="bridge"}`, logAllowlist()); err == nil {
		t.Error("expected rejection for stream selector without namespace label")
	}
}

func TestAuthorizeLogQL_MultipleStreamsOneNaked(t *testing.T) {
	q := `sum(rate({namespace="matrix"}[5m])) / sum(rate({app="bridge"}[5m]))`
	if err := lookout.AuthorizeLogQL(q, logAllowlist()); err == nil {
		t.Error("expected rejection when any stream selector is unscoped")
	}
}

// --- rejection: wildcard escapes ---

func TestAuthorizeLogQL_RegexWildcard(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{namespace=~".*"}`, logAllowlist()); err == nil {
		t.Error("expected rejection for .*")
	}
}

func TestAuthorizeLogQL_RegexPartialWildcard(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{namespace=~"ma.*"}`, logAllowlist()); err == nil {
		t.Error("expected rejection for ma.*")
	}
}

// --- rejection: non-allowlisted ---

func TestAuthorizeLogQL_EqualityNotInAllowlist(t *testing.T) {
	err := lookout.AuthorizeLogQL(`{namespace="kube-system"}`, logAllowlist())
	if err == nil {
		t.Error("expected rejection for kube-system")
	}
}

func TestAuthorizeLogQL_RegexAlternationMixed(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{namespace=~"matrix|kube-system"}`, logAllowlist()); err == nil {
		t.Error("expected rejection for mixed allowlist/non-allowlist alternation")
	}
}

// --- rejection: negative matchers ---

func TestAuthorizeLogQL_NotEqual(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{namespace!="kube-system"}`, logAllowlist()); err == nil {
		t.Error("expected rejection for != matcher")
	}
}

func TestAuthorizeLogQL_NotRegexp(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{namespace!~"kube-.*"}`, logAllowlist()); err == nil {
		t.Error("expected rejection for !~ matcher")
	}
}

// --- parse / extraction edge cases ---

func TestAuthorizeLogQL_EmptyQuery(t *testing.T) {
	if err := lookout.AuthorizeLogQL(``, logAllowlist()); err == nil {
		t.Error("expected rejection for empty query")
	}
}

func TestAuthorizeLogQL_UnbalancedBraces(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{namespace="matrix"`, logAllowlist()); err == nil {
		t.Error("expected rejection for unbalanced '{'")
	}
}

func TestAuthorizeLogQL_UnterminatedQuotedString(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{namespace="matrix`, logAllowlist()); err == nil {
		t.Error("expected rejection for unterminated string literal")
	}
}

func TestAuthorizeLogQL_EscapedQuoteInsideString(t *testing.T) {
	// Backslash-escaped quote inside the namespace value — the extractor must
	// not terminate the string early.
	// The selector itself is malformed (escaped-quote-included value
	// won't match any real namespace), so expect rejection from the matcher
	// parser, not from the extractor.
	q := `{namespace="mat\"rix"}`
	err := lookout.AuthorizeLogQL(q, logAllowlist())
	if err == nil {
		t.Error("expected rejection (string contains escaped quote; value not in allowlist)")
	}
	if strings.Contains(err.Error(), "unterminated") {
		t.Errorf("extractor terminated string early on escaped quote: %v", err)
	}
}

// --- rejection: empty allowlist is fail-closed ---

func TestAuthorizeLogQL_EmptyAllowlistDeniesEverything(t *testing.T) {
	empty := lookout.NewNamespaceAllowlist(nil)
	if err := lookout.AuthorizeLogQL(`{namespace="matrix"}`, empty); err == nil {
		t.Error("expected rejection on empty allowlist (fail-closed)")
	}
}

func TestAuthorizeLogQL_NilAllowlistDeniesEverything(t *testing.T) {
	if err := lookout.AuthorizeLogQL(`{namespace="matrix"}`, nil); err == nil {
		t.Error("expected rejection on nil allowlist (fail-closed)")
	}
}

package lookout_test

import (
	"reflect"
	"testing"

	"klazomenai/bridge/internal/tools/lookout"
)

func TestParseNamespaceAllowlist_Empty(t *testing.T) {
	a := lookout.ParseNamespaceAllowlist("")
	if a.Len() != 0 {
		t.Errorf("expected empty, got Len=%d", a.Len())
	}
}

func TestParseNamespaceAllowlist_SingleValue(t *testing.T) {
	a := lookout.ParseNamespaceAllowlist("matrix")
	if !a.Contains("matrix") {
		t.Error("expected matrix to be in allowlist")
	}
	if a.Contains("monitoring") {
		t.Error("expected monitoring NOT to be in allowlist")
	}
	if a.Len() != 1 {
		t.Errorf("expected Len=1, got %d", a.Len())
	}
}

func TestParseNamespaceAllowlist_MultipleValues(t *testing.T) {
	a := lookout.ParseNamespaceAllowlist("matrix,argocd,vault")
	for _, n := range []string{"matrix", "argocd", "vault"} {
		if !a.Contains(n) {
			t.Errorf("expected %q in allowlist", n)
		}
	}
	if a.Len() != 3 {
		t.Errorf("expected Len=3, got %d", a.Len())
	}
}

func TestParseNamespaceAllowlist_WhitespaceTrimmed(t *testing.T) {
	a := lookout.ParseNamespaceAllowlist(" matrix , argocd ,  vault  ")
	for _, n := range []string{"matrix", "argocd", "vault"} {
		if !a.Contains(n) {
			t.Errorf("expected %q in allowlist (whitespace should be trimmed)", n)
		}
	}
}

func TestParseNamespaceAllowlist_EmptyEntriesDropped(t *testing.T) {
	a := lookout.ParseNamespaceAllowlist("matrix,,argocd")
	if a.Len() != 2 {
		t.Errorf("expected Len=2 (empty entries discarded), got %d", a.Len())
	}
	// Empty string must not be allowlisted.
	if a.Contains("") {
		t.Error("empty string should never be in allowlist")
	}
}

func TestNamespaceAllowlist_NilMethods(t *testing.T) {
	var a *lookout.NamespaceAllowlist
	if a.Contains("matrix") {
		t.Error("nil allowlist should deny all")
	}
	if a.Len() != 0 {
		t.Errorf("nil Len should be 0, got %d", a.Len())
	}
	if names := a.Names(); names != nil {
		t.Errorf("nil Names should be nil, got %v", names)
	}
}

func TestNamespaceAllowlist_NamesSorted(t *testing.T) {
	a := lookout.NewNamespaceAllowlist([]string{"vault", "matrix", "argocd"})
	got := a.Names()
	want := []string{"argocd", "matrix", "vault"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

func TestNamespaceAllowlist_EmptyStringNotAllowlisted(t *testing.T) {
	// Defensive: even if somehow a zero-length rune slice reached Contains,
	// it must fail closed.
	a := lookout.NewNamespaceAllowlist([]string{"matrix"})
	if a.Contains("") {
		t.Error("empty string must never match")
	}
}

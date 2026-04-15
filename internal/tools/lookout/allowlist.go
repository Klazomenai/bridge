package lookout

import (
	"sort"
	"strings"
)

// NamespaceAllowlist is a set of Kubernetes namespace names that Lookout
// tools are permitted to query.
//
// The allowlist is configured via the LOOKOUT_NAMESPACE_ALLOWLIST env var
// (comma-separated) in cmd/bridge/main.go. If the env var is unset or empty,
// the Lookout tools register as stubs — consistent with the
// CHIPS_REPO_ALLOWLIST precedent.
type NamespaceAllowlist struct {
	set map[string]struct{}
}

// NewNamespaceAllowlist builds an allowlist from the given slice of names.
// Empty / whitespace-only entries are discarded. The returned allowlist is
// non-nil even when empty — callers distinguish via Len().
func NewNamespaceAllowlist(names []string) *NamespaceAllowlist {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		set[n] = struct{}{}
	}
	return &NamespaceAllowlist{set: set}
}

// ParseNamespaceAllowlist parses a comma-separated list into an allowlist.
// Leading/trailing whitespace on each entry is trimmed. Empty entries are
// discarded, so "a,,b" yields {a, b}.
func ParseNamespaceAllowlist(csv string) *NamespaceAllowlist {
	if csv == "" {
		return NewNamespaceAllowlist(nil)
	}
	return NewNamespaceAllowlist(strings.Split(csv, ","))
}

// Contains reports whether the given namespace is in the allowlist.
// Returns false on empty input.
func (a *NamespaceAllowlist) Contains(ns string) bool {
	if a == nil || ns == "" {
		return false
	}
	_, ok := a.set[ns]
	return ok
}

// Len returns the number of allowlisted namespaces.
func (a *NamespaceAllowlist) Len() int {
	if a == nil {
		return 0
	}
	return len(a.set)
}

// Names returns the allowlisted namespaces as a sorted slice. Intended for
// logging and error messages. Returns nil if the allowlist is empty.
func (a *NamespaceAllowlist) Names() []string {
	if a == nil || len(a.set) == 0 {
		return nil
	}
	out := make([]string, 0, len(a.set))
	for n := range a.set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

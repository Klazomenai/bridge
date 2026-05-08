package redact_test

import (
	"strings"
	"testing"

	"klazomenai/bridge/internal/tools/redact"
)

func TestRedactSingleSecretReplaced(t *testing.T) {
	out := redact.Redact("token=ghp_abc123 user=root", "ghp_abc123")
	if strings.Contains(out, "ghp_abc123") {
		t.Errorf("raw secret leaked: %q", out)
	}
	if !strings.Contains(out, redact.Sentinel) {
		t.Errorf("sentinel missing: %q", out)
	}
}

func TestRedactMultipleSecretsReplaced(t *testing.T) {
	out := redact.Redact(
		"gh=ghp_abc123 ssh=AAAA-priv-key",
		"ghp_abc123",
		"AAAA-priv-key",
	)
	if strings.Contains(out, "ghp_abc123") || strings.Contains(out, "AAAA-priv-key") {
		t.Errorf("raw secret leaked: %q", out)
	}
	if strings.Count(out, redact.Sentinel) != 2 {
		t.Errorf("expected 2 sentinel substitutions, got %q", out)
	}
}

func TestRedactEmptySecretIsSkipped(t *testing.T) {
	// Empty secret would otherwise match every position via
	// strings.ReplaceAll — explicitly skipped to allow unset config
	// values to pass through without conditional logic at the call site.
	in := "no-secrets-here"
	out := redact.Redact(in, "")
	if out != in {
		t.Errorf("empty secret altered output: %q → %q", in, out)
	}
}

func TestRedactNoSecretsReturnsInputUnchanged(t *testing.T) {
	in := "plain text"
	out := redact.Redact(in)
	if out != in {
		t.Errorf("no-args call altered output: %q → %q", in, out)
	}
}

func TestRedactSecretNotPresentReturnsInputUnchanged(t *testing.T) {
	in := "no token here"
	out := redact.Redact(in, "ghp_does_not_appear")
	if out != in {
		t.Errorf("missing-secret call altered output: %q → %q", in, out)
	}
}

func TestRedactRepeatedOccurrencesAllReplaced(t *testing.T) {
	out := redact.Redact("ghp_xyz one ghp_xyz two ghp_xyz", "ghp_xyz")
	if strings.Contains(out, "ghp_xyz") {
		t.Errorf("residual secret occurrence: %q", out)
	}
	if strings.Count(out, redact.Sentinel) != 3 {
		t.Errorf("expected 3 sentinel substitutions, got %q", out)
	}
}

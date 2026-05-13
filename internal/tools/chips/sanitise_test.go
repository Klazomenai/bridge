package chips_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"klazomenai/bridge/internal/tools/chips"
	"klazomenai/bridge/internal/tools/redact"
)

// TestChipsSanitisePerPatternPositiveAndNegative pins that
// chips.Sanitise — not just the underlying redact.Sanitise — both
// applies every named pattern to a positive case AND leaves a
// near-miss input unchanged at the chips boundary. Satisfies #83's
// AC: "Unit tests in internal/tools/chips/sanitise_test.go cover
// each pattern with at least one positive and one false-positive
// case". Negative fixtures mirror the per-pattern boundary cases in
// internal/tools/redact/redact_test.go and are pinned again here so
// the wrapper layer's contract is verified directly rather than
// indirectly through the redact tests.
func TestChipsSanitisePerPatternPositiveAndNegative(t *testing.T) {
	cases := []struct {
		name        string
		positive    string
		negative    string
		mustContain string
	}{
		{
			name:        "aws_access_key",
			positive:    "comment body: AKIATESTKEY012345678 planted by attacker",
			negative:    "operator note: AKIAtypo here in body",
			mustContain: "AKIA…REDACTED",
		},
		{
			name:        "github_token_ghp",
			positive:    "Hey check out this PAT: ghp_" + strings.Repeat("Z", 40) + " — please rotate",
			negative:    "stub token ghp_short in comment",
			mustContain: "ghp_…REDACTED",
		},
		{
			name:        "github_token_ghu",
			positive:    "leaked user token ghu_" + strings.Repeat("B", 40) + " end",
			negative:    "tok ghu_too_short for the class match",
			mustContain: "ghu_…REDACTED",
		},
		{
			name:        "github_pat",
			positive:    "found token github_pat_" + strings.Repeat("C", 30) + " in logs",
			negative:    "github_pat_short fixture",
			mustContain: "github_pat_…REDACTED",
		},
		{
			name:        "openai_anthropic_key",
			positive:    "claude key sk-ant-" + strings.Repeat("d", 40) + " in comment",
			negative:    "snippet sk-x truncated here",
			mustContain: "sk-…REDACTED",
		},
		{
			name:        "slack_token",
			positive:    "slack bot tok xoxb-1234567890-abcde-fghijk in body",
			negative:    "fixture xoxq-not-a-slack-shape here",
			mustContain: "xoxb-…REDACTED",
		},
		{
			name:        "jwt",
			positive:    "auth=eyJ-TEST-HEADER-PART.eyJ-TEST-PAYLOAD-PART.TEST-SIGNATURE-PART rest",
			negative:    "auth=eyJonly-no-second-segment here",
			mustContain: "JWT-REDACTED",
		},
		{
			name:        "pem_block",
			positive:    "found:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAA\nKCAQEA\n-----END RSA PRIVATE KEY-----\nin comment",
			negative:    "no envelope here, just the word BEGIN by itself",
			mustContain: "-----BEGIN … KEY----- REDACTED -----END … KEY-----",
		},
		{
			name:        "bearer_token",
			positive:    "Authorization: Bearer abc123def456ghi.789-jkl_mno",
			negative:    "the bearer of bad news arrived early",
			mustContain: "Bearer REDACTED",
		},
		{
			name:        "password_assignment",
			positive:    "config snippet password: hunter2-but-longer",
			negative:    "passwordless flow needs more docs",
			mustContain: "password: REDACTED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/positive", func(t *testing.T) {
			out := chips.Sanitise(tc.positive)
			if !strings.Contains(out, tc.mustContain) {
				t.Errorf("expected %q in output, got: %q", tc.mustContain, out)
			}
		})
		t.Run(tc.name+"/negative", func(t *testing.T) {
			out := chips.Sanitise(tc.negative)
			if out != tc.negative {
				t.Errorf("near-miss input altered at chips boundary: %q → %q",
					tc.negative, out)
			}
		})
	}
}

func TestChipsSanitisePreservesInnocentText(t *testing.T) {
	// Real PR titles and bodies must pass through unchanged.
	// "Bearer" without a token, "password" as a noun, AKIA-shorter
	// strings, etc. all stay intact.
	inputs := []string{
		"feat(crew): add a delegate path",
		"This PR fixes the bearer of bad news pattern",
		"My passwordless flow needs more docs",
		"The AKIA prefix is short for AWS Key Identity ABC",
		"Discussion of sk- prefixed identifiers in research",
	}
	for _, in := range inputs {
		t.Run(in[:min(len(in), 40)], func(t *testing.T) {
			out := chips.Sanitise(in)
			if out != in {
				t.Errorf("benign text altered: %q → %q", in, out)
			}
		})
	}
}

func TestChipsSanitiseIdempotence(t *testing.T) {
	// Chips Sanitise idempotence is load-bearing for #129's
	// orchestrator-level safety floor: the floor applies the same
	// pattern set, so a Chips-already-Sanitised output must not be
	// disturbed when it lands at the floor.
	inputs := []string{
		"plain pr title",
		"comment with AKIATESTKEY012345678 planted",
		"long body ghp_" + strings.Repeat("A", 40) + " somewhere",
		"Authorization: Bearer xyz-123",
		"in env: DATABASE_PASSWORD=hunter2",
	}
	for _, in := range inputs {
		t.Run(in[:min(len(in), 40)], func(t *testing.T) {
			once := chips.Sanitise(in)
			twice := chips.Sanitise(once)
			if once != twice {
				t.Errorf("chips.Sanitise not idempotent:\n  once:  %q\n  twice: %q", once, twice)
			}
		})
	}
}

func TestChipsSanitiseEmptyInputUnchanged(t *testing.T) {
	// Smoke test that the chips wrapper passes empty input through
	// without introducing its own failure surface ahead of the
	// redact-level recover. The fail-closed contract itself (panic
	// recovery, SanitiserErrorReplacement substitution) is exercised
	// in internal/tools/redact's TestSanitiseFailClosedOnPanic via
	// a nil-regex Pattern injection through SanitiseWith — chips
	// inherits that contract directly because allChipsPatterns
	// composes a slice that ends up at the same SanitiseWith call.
	if out := chips.Sanitise(""); out != "" {
		t.Errorf("empty input altered: %q", out)
	}
}

func TestChipsSanitiseRespectsSharedMaxBytes(t *testing.T) {
	// chips.Sanitise inherits redact.MaxSanitiserInputBytes via
	// redact.SanitiseWith. A token planted past the cap must be
	// truncated away, not surfaced or processed.
	tail := "AKIATESTKEY012345678"
	in := strings.Repeat("x", redact.MaxSanitiserInputBytes) + " " + tail
	out := chips.Sanitise(in)
	if strings.Contains(out, tail) {
		t.Error("tail token surfaced past truncation in chips wrapper")
	}
	if len(out) > redact.MaxSanitiserInputBytes {
		t.Errorf("chips output exceeds shared cap: %d bytes", len(out))
	}
}

// SanitiseOutput (the exec.go wrapper called by every gh_* tool) must
// chain redact.Redact (substring) and chips.Sanitise (pattern). The
// next four tests pin the chained behaviour: known-token redaction
// keeps working; planted token-shaped strings get pattern-scrubbed;
// both together for a realistic mixed payload.

func TestSanitiseOutputChainKnownTokenStillRedacted(t *testing.T) {
	// Regression assertion for the original #152 contract: when a
	// known GITHUB_TOKEN value is supplied, it is substring-redacted
	// to the [REDACTED] sentinel by redact.Redact's first pass, even
	// when the value's shape does not match any default Sanitise
	// pattern. The token below contains `-` characters inside the
	// `ghp_` body, which falls outside the github_token pattern's
	// `[A-Za-z0-9]{36,}` character class — so the substring path is
	// the only mechanism that can catch it. Asserts the chain's
	// first stage in isolation.
	token := "ghp_known-test-secret-with-dashes-only-substring-catches-it"
	out := chips.SanitiseOutputForTest("emit "+token+" and stop", token, "")
	if strings.Contains(out, token) {
		t.Errorf("known token surfaced: %q", out)
	}
	if !strings.Contains(out, redact.Sentinel) {
		t.Errorf("expected %q sentinel in output: %q", redact.Sentinel, out)
	}
}

func TestSanitiseOutputChainPlantedTokenPatternRedacted(t *testing.T) {
	// The new #83 contract: a token-SHAPE the operator never supplied
	// as a known secret (because it was planted by a third party in a
	// GitHub comment body) gets caught by pattern matching even though
	// the caller passes an empty / different token.
	planted := "AKIA" + strings.Repeat("Q", 16)
	out := chips.SanitiseOutputForTest("attacker planted: "+planted+" here", "", "")
	if strings.Contains(out, planted) {
		t.Errorf("planted token surfaced: %q", out)
	}
	if !strings.Contains(out, "AKIA…REDACTED") {
		t.Errorf("expected pattern sentinel: %q", out)
	}
}

func TestSanitiseOutputChainBothPathsTogether(t *testing.T) {
	// A realistic mixed payload: the gh-shell command sometimes echoes
	// the configured token, and an attacker plants a different
	// secret-shape in an issue body. Both must redact.
	token := "ghp_realOperatorTokenValueKnownLiterally"
	planted := "AKIA" + strings.Repeat("J", 16)
	in := "tool error: " + token + "\nissue body: " + planted + "\n"
	out := chips.SanitiseOutputForTest(in, token, "")
	if strings.Contains(out, token) {
		t.Errorf("known token surfaced: %q", out)
	}
	if strings.Contains(out, planted) {
		t.Errorf("planted token surfaced: %q", out)
	}
	if !strings.Contains(out, redact.Sentinel) {
		t.Errorf("expected substring sentinel: %q", out)
	}
	if !strings.Contains(out, "AKIA…REDACTED") {
		t.Errorf("expected pattern sentinel: %q", out)
	}
}

func TestSanitiseOutputThreadsToolNameToLogWhenSet(t *testing.T) {
	// AC10 (#83): production callers in gh_*.go / git_*.go pass
	// t.Name() as the third arg to sanitiseOutput. The log line for
	// each pattern match must carry that tool name in the `tool`
	// attribute (operators trace per-tool redaction frequency from
	// Loki without needing to correlate via timestamps).
	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	defer slog.SetDefault(original)

	planted := "AKIA" + strings.Repeat("Q", 16)
	_ = chips.SanitiseOutputForTest("leaked "+planted+" here", "", "gh_issue_view")

	logOut := buf.String()
	if !strings.Contains(logOut, `"tool":"gh_issue_view"`) {
		t.Errorf("expected tool=gh_issue_view in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"field":"output"`) {
		t.Errorf("expected field=output in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"pattern_name":"aws_access_key"`) {
		t.Errorf("expected pattern_name=aws_access_key in log, got: %s", logOut)
	}
}

func TestSanitiseOutputSilentWhenToolNameEmpty(t *testing.T) {
	// Test/SanitiseOutputForTest callers pass tool="" — sanitisation
	// runs but no slog line is emitted. Pins that tests don't
	// pollute production log output AND that empty tool string is
	// a safe no-log signal (not a tool literally named "").
	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	defer slog.SetDefault(original)

	planted := "AKIA" + strings.Repeat("Q", 16)
	out := chips.SanitiseOutputForTest("leaked "+planted+" here", "", "")

	if buf.Len() != 0 {
		t.Errorf("expected silent sanitisation with empty tool, got log: %s",
			buf.String())
	}
	// Sanity check: redaction still happened (the absence of the
	// raw planted token in `out` proves the silent path still
	// applies the patterns).
	if strings.Contains(out, planted) {
		t.Errorf("silent path did not sanitise: %q", out)
	}
}

func TestSanitiseOutputChainEmptyTokenStillSanitisesPatterns(t *testing.T) {
	// Pre-#83 behaviour: empty token meant no substring redaction
	// happened. Post-#83: pattern-based Sanitise still runs even when
	// the caller has no known secret to redact.
	in := "comment: ghp_" + strings.Repeat("M", 40) + " end"
	out := chips.SanitiseOutputForTest(in, "", "")
	if !strings.Contains(out, "ghp_…REDACTED") {
		t.Errorf("expected pattern sentinel with empty token: %q", out)
	}
}

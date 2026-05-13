package redact_test

import (
	"bytes"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"klazomenai/bridge/internal/tools/redact"
)

// captureSlog routes sanitiser emissions to a buffer via the
// package-level redact.SetLogger swap (NOT slog.SetDefault, which
// would mutate process-global state and could race with other
// packages' tests under hypothetical parallel execution). Returns
// the buffer and a restore function — defer the restore.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	restore := redact.SetLogger(logger)
	return &buf, restore
}

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

func TestRedactOverlappingSecretsLongerWinsRegardlessOfOrder(t *testing.T) {
	// Without internal length-descending sort, the caller-supplied
	// order ["foo", "foobar"] would match "foo" first (inside
	// "foobar") and leave "[REDACTED]bar" — a partial secret leak.
	// The internal sort guarantees the longest match wins, so both
	// caller orderings produce identical output.
	cases := []struct {
		name    string
		secrets []string
	}{
		{"shorter first", []string{"foo", "foobar"}},
		{"longer first", []string{"foobar", "foo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := redact.Redact("foo and foobar are secrets", tc.secrets...)
			if strings.Contains(out, "foobar") {
				t.Errorf("partial redaction left foobar: %q", out)
			}
			if strings.Contains(out, "foo ") {
				t.Errorf("standalone foo not redacted: %q", out)
			}
			// A residual "bar" implies foobar's tail leaked through
			// partial redaction.
			if strings.Contains(out, "bar") {
				t.Errorf("residual bar suffix (partial leak): %q", out)
			}
			if strings.Count(out, redact.Sentinel) != 2 {
				t.Errorf("expected 2 sentinel substitutions, got %q", out)
			}
		})
	}
}

// --- Sanitise tests ---

// Each row carries a positive case (input that MUST be redacted) and
// a near-miss negative case (input that MUST NOT be redacted) for one
// pattern. The negative cases pin the regex boundaries: shorter than
// the minimum length, wrong prefix shape, or missing the structural
// delimiter that anchors the pattern.
func TestSanitisePerPatternPositiveAndNegative(t *testing.T) {
	cases := []struct {
		name        string
		positive    string
		negative    string
		mustContain string // must appear in Sanitised positive output
	}{
		{
			name:        "aws_access_key",
			positive:    "key=AKIATESTKEY012345678 rest",
			negative:    "key=AKIAtypo rest",
			mustContain: "AKIA…REDACTED",
		},
		{
			name:        "github_token_ghp",
			positive:    "token=ghp_" + strings.Repeat("A", 36) + " end",
			negative:    "token=ghp_short end",
			mustContain: "ghp_…REDACTED",
		},
		{
			name:        "github_token_ghs",
			positive:    "token=ghs_" + strings.Repeat("z", 40) + " end",
			negative:    "token=ghs_too_short end",
			mustContain: "ghs_…REDACTED",
		},
		{
			name:        "github_pat",
			positive:    "token=github_pat_" + strings.Repeat("X", 30) + " end",
			negative:    "token=github_pat_short end",
			mustContain: "github_pat_…REDACTED",
		},
		{
			name:        "openai_anthropic_key",
			positive:    "key=sk-" + strings.Repeat("a", 40) + " end",
			negative:    "key=sk-tiny end",
			mustContain: "sk-…REDACTED",
		},
		{
			name:        "openai_anthropic_key_ant_prefix",
			positive:    "key=sk-ant-" + strings.Repeat("b", 40) + " end",
			negative:    "key=sk-ant-x end",
			mustContain: "sk-…REDACTED",
		},
		{
			name:        "slack_token_xoxb",
			positive:    "tok=xoxb-1234567890-abcde-fghijk end",
			negative:    "tok=xoxq-not-a-slack-shape end",
			mustContain: "xoxb-…REDACTED",
		},
		{
			name:        "jwt",
			positive:    "auth=eyJ-TEST-HEADER-PART.eyJ-TEST-PAYLOAD-PART.TEST-SIGNATURE-PART end",
			negative:    "auth=eyJonly.notajwt end",
			mustContain: "JWT-REDACTED",
		},
		{
			name: "pem_block",
			positive: "prefix\n-----BEGIN RSA PRIVATE KEY-----\n" +
				"MIIEpAIBAAKCAQEAxyz\n" +
				"more lines here\n" +
				"-----END RSA PRIVATE KEY-----\nsuffix",
			negative:    "no envelope here, just BEGIN words",
			mustContain: "-----BEGIN … KEY----- REDACTED -----END … KEY-----",
		},
		{
			name:        "bearer_token",
			positive:    "Authorization: Bearer abc123.def-456_xyz",
			negative:    "berating someone with words",
			mustContain: "Bearer REDACTED",
		},
		{
			name:        "password_assignment_colon",
			positive:    "config: password: hunter2-on-deck",
			negative:    "passwordless flow note",
			mustContain: "password: REDACTED",
		},
		{
			name:        "password_assignment_equals",
			positive:    "DATABASE_PASSWORD=p@ssw0rd-1!",
			negative:    "passwordy text",
			mustContain: "PASSWORD=REDACTED",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/positive", func(t *testing.T) {
			out := redact.Sanitise(tc.positive)
			if !strings.Contains(out, tc.mustContain) {
				t.Errorf("expected %q in output, got: %q", tc.mustContain, out)
			}
		})
		t.Run(tc.name+"/negative", func(t *testing.T) {
			out := redact.Sanitise(tc.negative)
			if out != tc.negative {
				t.Errorf("near-miss input altered: %q → %q", tc.negative, out)
			}
		})
	}
}

func TestSanitiseIdempotence(t *testing.T) {
	// Idempotence is the contract that the orchestrator-level safety
	// floor (#129) relies on: per-tool sanitisation runs first, the
	// floor runs again on the same content, and the second pass must
	// be a no-op for un-tainted strings AND for strings whose tokens
	// were already replaced. Without this, the sentinel itself could
	// be re-matched and re-Sanitised into something unrecognisable.
	inputs := []string{
		"plain text without secrets",
		"AKIATESTKEY012345678 one",
		"token=ghp_" + strings.Repeat("X", 40),
		// Bearer fixture is 30 chars after `Bearer ` so it actually
		// triggers the `{16,}` minimum and exercises the bearer
		// redaction's idempotence (a sub-cap bearer like
		// "Bearer abc-123" would skip the pattern entirely and the
		// idempotence assertion would be vacuous).
		"Authorization: Bearer abc-123def-456-789-token-here",
		// Slack matches the `xox([baprs])-[A-Za-z0-9-]{10,}` shape;
		// pins idempotence of the new capture-group replacement.
		"slack tok=xoxb-1234567890-abcde-fghij end",
		"password: hunter2",
		"key=sk-ant-" + strings.Repeat("a", 40),
		"jwt=eyJ-TEST-HEADER-PART.eyJ-TEST-PAYLOAD-PART.TEST-SIGNATURE-PART",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEpQ\n-----END RSA PRIVATE KEY-----",
		"DATABASE_PASSWORD=secret123",
	}
	for _, in := range inputs {
		t.Run(in[:min(len(in), 40)], func(t *testing.T) {
			once := redact.Sanitise(in)
			twice := redact.Sanitise(once)
			if once != twice {
				t.Errorf("not idempotent:\n  once:  %q\n  twice: %q", once, twice)
			}
		})
	}
}

func TestSanitiseTruncatesInputAtMaxBytes(t *testing.T) {
	// A token planted past the byte cap must NOT appear in the
	// Sanitised output — truncation makes downstream consumers blind
	// to the tail, which is the intended trade-off against regex-DoS
	// amplification on attacker-controlled bodies.
	prefix := strings.Repeat("a", redact.MaxSanitiserInputBytes)
	tailToken := "AKIATESTKEY012345678" // valid AWS key shape (20 chars)
	in := prefix + " " + tailToken
	if len(in) <= redact.MaxSanitiserInputBytes {
		t.Fatalf("test fixture broken: in is %d bytes, must exceed cap", len(in))
	}

	out := redact.Sanitise(in)

	if len(out) > redact.MaxSanitiserInputBytes {
		t.Errorf("output exceeds MaxSanitiserInputBytes: got %d bytes", len(out))
	}
	if strings.Contains(out, tailToken) {
		t.Errorf("tail token surfaced past truncation: %q", out)
	}
	if strings.Contains(out, "AKIA…REDACTED") {
		t.Error("tail token was processed (should have been truncated away before pattern matching)")
	}
}

func TestSanitiseTruncationPreservesShortInputs(t *testing.T) {
	// Inputs at or below the cap must pass through pattern matching
	// untouched in length terms (modulo the pattern replacements).
	in := "token=AKIATESTKEY012345678 rest"
	out := redact.Sanitise(in)
	if !strings.Contains(out, "AKIA…REDACTED") {
		t.Errorf("expected redaction of short input, got: %q", out)
	}
}

func TestSanitiseOutputBoundedAtCapEvenWithExpandingReplacements(t *testing.T) {
	// Some pattern replacements add bytes to the matched span — the
	// U+2026 ellipsis sentinel is 3 UTF-8 bytes where the originating
	// pattern character class only accepts 1-byte ASCII. Stack enough
	// minimal-length matches to fill the input cap and the naive
	// (no output re-truncation) implementation would produce output
	// exceeding MaxSanitiserInputBytes by ~1 byte per match. The
	// function caps output at MaxSanitiserInputBytes so downstream
	// consumers (notably the orchestrator-level safety floor in #129)
	// see a known byte budget regardless of input shape.
	unit := "xoxb-1234567890 " // 16 bytes: 15-byte minimal match + space
	units := redact.MaxSanitiserInputBytes / len(unit)
	in := strings.Repeat(unit, units)
	if len(in) != redact.MaxSanitiserInputBytes {
		t.Fatalf("fixture broken: in is %d bytes, expected %d",
			len(in), redact.MaxSanitiserInputBytes)
	}

	out := redact.Sanitise(in)

	if len(out) > redact.MaxSanitiserInputBytes {
		t.Errorf("output exceeds cap despite re-truncation: got %d bytes, cap %d",
			len(out), redact.MaxSanitiserInputBytes)
	}
	// Pin that the test fixture actually exercised the replacement
	// path — without this, an inert input could trivially pass the
	// cap assertion.
	if !strings.Contains(out, "xoxb-…REDACTED") {
		t.Error("test fixture did not produce slack-token replacement; assertion above is vacuous")
	}
}

func TestSanitiseEmptyInput(t *testing.T) {
	if out := redact.Sanitise(""); out != "" {
		t.Errorf("empty input altered: %q", out)
	}
}

func TestSanitiseFailClosedOnPanic(t *testing.T) {
	// A panic-inducing pattern (nil Regex) routed through SanitiseWith
	// MUST return SanitiserErrorReplacement, NOT the partially-Sanitised
	// or un-Sanitised input. Bridge never passes un-Sanitised content
	// to Claude, even when its own infrastructure fails — this test
	// pins that contract.
	panicPatterns := []redact.Pattern{
		{Name: "nil_regex_panic", Regex: nil, Replacement: "x"},
	}
	out := redact.SanitiseWith("token=ghp_abc123", panicPatterns)
	if out != redact.SanitiserErrorReplacement {
		t.Errorf("expected SanitiserErrorReplacement %q, got %q",
			redact.SanitiserErrorReplacement, out)
	}
}

func TestSanitiseWithEmptyPatternsUnderCapReturnsInputUnchanged(t *testing.T) {
	// Below the cap, an empty pattern set should short-circuit
	// replacement and leave input bytes intact.
	in := "anything here including AKIATESTKEY012345678"
	out := redact.SanitiseWith(in, nil)
	if out != in {
		t.Errorf("empty pattern set altered under-cap input: %q → %q", in, out)
	}
}

func TestSanitiseWithEmptyPatternsOverCapStillTruncates(t *testing.T) {
	// The cap is enforced unconditionally — even with no patterns
	// to apply, an oversized input is truncated to MaxSanitiserInputBytes
	// before any pattern loop runs. This pins the contract that the
	// length ceiling is a property of SanitiseWith itself, not of the
	// pattern set.
	in := strings.Repeat("a", redact.MaxSanitiserInputBytes+1000)
	out := redact.SanitiseWith(in, nil)
	if len(out) != redact.MaxSanitiserInputBytes {
		t.Errorf("over-cap input not truncated to cap: got %d bytes, want %d",
			len(out), redact.MaxSanitiserInputBytes)
	}
	if out != in[:redact.MaxSanitiserInputBytes] {
		t.Error("over-cap truncated output differs from prefix of original input")
	}
}

func TestSanitiseWithCustomPattern(t *testing.T) {
	// Demonstrates the SanitiseWith extension surface: a per-crew
	// pattern slice can be passed alongside or instead of the shared
	// redact default pattern set (DefaultPatterns / Sanitise). Used
	// by per-crew Sanitisers (chips, crest, maren) to add their own
	// surface-specific patterns.
	custom := []redact.Pattern{
		{
			Name:        "deck_chat_session",
			Regex:       regexp.MustCompile(`dc_session_[0-9a-f]{16}`),
			Replacement: "dc_session_…REDACTED",
		},
	}
	in := "ref=dc_session_deadbeefcafef00d done"
	out := redact.SanitiseWith(in, custom)
	if !strings.Contains(out, "dc_session_…REDACTED") {
		t.Errorf("custom pattern did not apply: %q", out)
	}
}

func TestDefaultPatternsReturnsDefensiveCopy(t *testing.T) {
	// Without the defensive copy, mutating the returned slice would
	// permanently change the package default — accidental append in
	// an init() somewhere could break sanitisation for all callers,
	// and concurrent reads against a mutated slice can race.
	//
	// The test mutates the aws_access_key pattern (located by name,
	// not by index) so that adding/reordering patterns in the default
	// set does not silently weaken this assertion: a future reorder
	// would otherwise mutate a different pattern entry and the AKIA
	// sanity-check below would still pass even if defensive copy
	// were broken.
	copy1 := redact.DefaultPatterns()
	copy2 := redact.DefaultPatterns()

	if len(copy1) == 0 {
		t.Fatal("DefaultPatterns returned empty slice; the default set must have at least one pattern")
	}
	if len(copy1) != len(copy2) {
		t.Fatalf("DefaultPatterns calls returned different lengths: %d vs %d",
			len(copy1), len(copy2))
	}

	findByName := func(slice []redact.Pattern, name string) (int, bool) {
		for i, p := range slice {
			if p.Name == name {
				return i, true
			}
		}
		return -1, false
	}

	idx1, ok := findByName(copy1, "aws_access_key")
	if !ok {
		t.Fatal("aws_access_key pattern not present in DefaultPatterns(); update the test fixture or restore the pattern")
	}

	// Mutate the located entry in copy1. The Pattern struct fields
	// are value-copied by the make+copy sequence, so this must not
	// affect copy2 OR the internal default set.
	original := copy1[idx1]
	copy1[idx1] = redact.Pattern{Name: "MUTATED", Replacement: "MUTATED"}

	idx2, ok := findByName(copy2, "aws_access_key")
	if !ok {
		t.Fatal("aws_access_key pattern disappeared from copy2; DefaultPatterns calls disagree")
	}
	if copy2[idx2].Name != original.Name {
		t.Errorf("DefaultPatterns returned shared backing slice: copy1 mutation affected copy2 (%q != %q)",
			copy2[idx2].Name, original.Name)
	}

	// Sanitise must still behave per the pre-mutation default set —
	// pin via the AKIA fixture that the internal aws_access_key
	// pattern survived copy1's mutation.
	out := redact.Sanitise("planted AKIATESTKEY012345678 here")
	if !strings.Contains(out, "AKIA…REDACTED") {
		t.Errorf("Sanitise was disturbed by DefaultPatterns copy mutation: %q", out)
	}
}

func TestSanitiseEmitsLogPerPatternMatchWithAttrs(t *testing.T) {
	// AC10 (#83): each pattern that fires emits one slog.Info line
	// with the caller's attrs (tool + field) plus pattern_name +
	// count. Two distinct patterns + 2 AWS matches + 1 GH token match
	// produces exactly 2 log lines (one per pattern), with counts 2
	// and 1 respectively.
	buf, restore := captureSlog(t)
	defer restore()

	in := "leaked AKIATESTKEY012345678 and AKIATESTKEY999999999 and tok ghp_" +
		strings.Repeat("A", 40) + " end"
	_ = redact.Sanitise(in,
		slog.String("tool", "test_tool"),
		slog.String("field", "output"),
	)

	logOut := buf.String()
	if !strings.Contains(logOut, `"tool":"test_tool"`) {
		t.Errorf("expected tool attr in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"field":"output"`) {
		t.Errorf("expected field attr in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"pattern_name":"aws_access_key"`) {
		t.Errorf("expected aws_access_key pattern_name in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"pattern_name":"github_token"`) {
		t.Errorf("expected github_token pattern_name in log, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"count":2`) {
		t.Errorf("expected count=2 for two AWS matches, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"count":1`) {
		t.Errorf("expected count=1 for one GH token match, got: %s", logOut)
	}
	if !strings.Contains(logOut, `"msg":"sanitiser_redaction"`) {
		t.Errorf("expected sanitiser_redaction msg in log, got: %s", logOut)
	}

	// Critical: the matched values themselves MUST NOT appear in the
	// log output (would defeat the purpose of redacting them).
	if strings.Contains(logOut, "AKIATESTKEY012345678") {
		t.Error("raw AWS key leaked into log output")
	}
	if strings.Contains(logOut, strings.Repeat("A", 40)) {
		t.Error("raw GH token leaked into log output")
	}
}

func TestSanitiseSilentWhenNoLogAttrs(t *testing.T) {
	// AC10 (#83) inverse: no log attrs → no emission. The
	// orchestrator-level safety floor (#129) and existing callers
	// that pre-date logging stay silent so they don't double-log
	// every redaction event.
	buf, restore := captureSlog(t)
	defer restore()

	in := "leaked AKIATESTKEY012345678 and ghp_" + strings.Repeat("Z", 40)
	_ = redact.Sanitise(in) // no log attrs

	if buf.Len() != 0 {
		t.Errorf("expected silent sanitisation with no attrs, got log output: %s",
			buf.String())
	}
}

func TestSanitiseNoLogLineWhenZeroMatches(t *testing.T) {
	// Even with attrs provided, a pattern that finds no matches must
	// not emit a log line — log entries should map 1:1 to patterns
	// that actually fired (avoids noise when sanitising benign text).
	buf, restore := captureSlog(t)
	defer restore()

	in := "completely benign text with no token shapes whatsoever"
	_ = redact.Sanitise(in,
		slog.String("tool", "test_tool"),
		slog.String("field", "output"),
	)

	if buf.Len() != 0 {
		t.Errorf("expected no log entries when zero patterns matched, got: %s",
			buf.String())
	}
}

func TestSetLoggerNilResetsToDefault(t *testing.T) {
	// SetLogger(nil) MUST treat the argument as a reset back to
	// slog.Default — installing a nil logger would otherwise propagate
	// to getLogger() and the next Sanitise emission would panic
	// inside the recover handler itself, escaping the fail-closed
	// guarantee that's the whole point of the recover.
	restore := redact.SetLogger(nil)
	defer restore()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Sanitise panicked after SetLogger(nil): %v", r)
		}
	}()
	// Trigger both code paths that go through getLogger():
	// (1) the per-pattern emission via Sanitise + attrs.
	_ = redact.Sanitise("planted AKIATESTKEY012345678 here",
		slog.String("tool", "x"),
		slog.String("field", "y"),
	)
	// (2) the panic-recover emission inside SanitiseWith.
	out := redact.SanitiseWith("anything", []redact.Pattern{
		{Name: "nil_regex_panic", Regex: nil, Replacement: "x"},
	},
		slog.String("tool", "x"),
		slog.String("field", "y"),
	)
	if out != redact.SanitiserErrorReplacement {
		t.Errorf("expected fail-closed replacement after panic; got %q", out)
	}
}

func TestSetLoggerRestoreUnwindsToPreviousLogger(t *testing.T) {
	// Each SetLogger call returns a restore that rolls back to the
	// logger installed at the moment of THAT call (not the original
	// default). Nested install/restore unwinds in LIFO order; this
	// matters when test helpers compose.
	var first, second bytes.Buffer
	firstLogger := slog.New(slog.NewJSONHandler(&first, nil))
	secondLogger := slog.New(slog.NewJSONHandler(&second, nil))

	restore1 := redact.SetLogger(firstLogger)
	restore2 := redact.SetLogger(secondLogger)

	// Emit while secondLogger is active → goes to `second`
	_ = redact.Sanitise("a AKIATESTKEY012345678",
		slog.String("tool", "x"), slog.String("field", "y"))
	if first.Len() != 0 {
		t.Errorf("first logger captured under second's install: %s", first.String())
	}
	if second.Len() == 0 {
		t.Error("second logger captured nothing")
	}

	// Restore2 unwinds to firstLogger
	restore2()
	secondLenAtSwap := second.Len()
	_ = redact.Sanitise("b AKIATESTKEY012345678",
		slog.String("tool", "x"), slog.String("field", "y"))
	if first.Len() == 0 {
		t.Error("first logger received nothing after restore2")
	}
	if second.Len() != secondLenAtSwap {
		t.Error("second logger still receiving after restore2")
	}

	restore1()
}

func TestSanitiserConstantsAreLoadBearing(t *testing.T) {
	// These constants are part of the public contract (referenced by
	// chips, the orchestrator floor #129, and future per-crew
	// Sanitisers). Pin them so a careless edit cannot silently relax
	// the cap or change the panic-replacement marker.
	if redact.MaxSanitiserInputBytes != 65536 {
		t.Errorf("MaxSanitiserInputBytes drifted: got %d", redact.MaxSanitiserInputBytes)
	}
	if redact.SanitiserErrorReplacement != "[SANITISER ERROR — content suppressed]" {
		t.Errorf("SanitiserErrorReplacement drifted: %q", redact.SanitiserErrorReplacement)
	}
}

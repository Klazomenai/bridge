package chips_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"klazomenai/bridge/internal/tools/chips"
)

func mockExec(output string, err error) (chips.ExecFn, *[]string) {
	var calls []string
	fn := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte(output), err
	}
	return fn, &calls
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

var testAllowlist = chips.ParseRepoAllowlist("klazomenai/bridge,klazomenai/deck-chat")

// --- RepoAllowlist tests ---

func TestRepoAllowlistAllowed(t *testing.T) {
	if err := testAllowlist.Check("klazomenai", "bridge"); err != nil {
		t.Fatalf("expected allowed, got: %v", err)
	}
}

func TestRepoAllowlistDenied(t *testing.T) {
	if err := testAllowlist.Check("klazomenai", "secret-repo"); err == nil {
		t.Fatal("expected error for disallowed repo")
	}
}

func TestRepoAllowlistCaseInsensitive(t *testing.T) {
	if err := testAllowlist.Check("Klazomenai", "Bridge"); err != nil {
		t.Fatalf("expected case-insensitive match, got: %v", err)
	}
}

func TestParseRepoAllowlistEmpty(t *testing.T) {
	list := chips.ParseRepoAllowlist("")
	if len(list) != 0 {
		t.Errorf("expected empty allowlist, got %d entries", len(list))
	}
}

// --- sanitiseOutput tests ---

func TestSanitiseOutputStripsToken(t *testing.T) {
	output := "some output with ghp_abc123secret in it"
	result := chips.SanitiseOutputForTest(output, "ghp_abc123secret", "")
	if strings.Contains(result, "ghp_abc123secret") {
		t.Error("output still contains token")
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Error("expected [REDACTED] in output")
	}
}

func TestSanitiseOutputEmptyToken(t *testing.T) {
	output := "safe output"
	result := chips.SanitiseOutputForTest(output, "", "")
	if result != output {
		t.Errorf("expected unchanged output, got %q", result)
	}
}

// --- GHIssueListTool tests ---

func TestGHIssueListSuccess(t *testing.T) {
	fn, calls := mockExec(`[{"number":1,"title":"test"}]`, nil)
	tool := chips.NewGHIssueListTool(fn, testAllowlist, "")

	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge"})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "test") {
		t.Errorf("expected output to contain 'test', got %q", out)
	}
	cmd := (*calls)[0]
	if !strings.Contains(cmd, "gh issue list") {
		t.Errorf("unexpected command: %q", cmd)
	}
	if !strings.Contains(cmd, "-R klazomenai/bridge") {
		t.Errorf("expected -R flag, got: %q", cmd)
	}
	if !strings.Contains(cmd, "--state open") {
		t.Errorf("expected default state open, got: %q", cmd)
	}
}

func TestGHIssueListDeniedRepo(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := chips.NewGHIssueListTool(fn, testAllowlist, "")

	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "secret"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for disallowed repo")
	}
	if !strings.Contains(err.Error(), "not in the allowed list") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- GHIssueViewTool tests ---

func TestGHIssueViewSuccess(t *testing.T) {
	fn, calls := mockExec(`{"number":42,"title":"bug"}`, nil)
	tool := chips.NewGHIssueViewTool(fn, testAllowlist, "")

	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "number": 42})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "bug") {
		t.Errorf("unexpected output: %q", out)
	}
	if !strings.Contains((*calls)[0], "issue view 42") {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

func TestGHIssueViewInvalidNumber(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := chips.NewGHIssueViewTool(fn, testAllowlist, "")

	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "number": 0})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for number <= 0")
	}
}

// --- GHPRListTool tests ---

func TestGHPRListSuccess(t *testing.T) {
	fn, calls := mockExec(`[{"number":1}]`, nil)
	tool := chips.NewGHPRListTool(fn, testAllowlist, "")

	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "state": "merged"})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains((*calls)[0], "--state merged") {
		t.Errorf("expected state merged, got: %q", (*calls)[0])
	}
}

// --- GHPRViewTool tests ---

func TestGHPRViewSuccess(t *testing.T) {
	fn, calls := mockExec(`{"number":10}`, nil)
	tool := chips.NewGHPRViewTool(fn, testAllowlist, "")

	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "number": 10})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains((*calls)[0], "pr view 10") {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

// --- GHPRChecksTool tests ---

func TestGHPRChecksSuccess(t *testing.T) {
	fn, calls := mockExec(`[{"name":"test","conclusion":"success"}]`, nil)
	tool := chips.NewGHPRChecksTool(fn, testAllowlist, "")

	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "number": 5})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains((*calls)[0], "pr checks 5") {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

func TestGHPRChecksInvalidNumber(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := chips.NewGHPRChecksTool(fn, testAllowlist, "")

	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "number": -1})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for negative number")
	}
}

// --- GitLogTool tests ---

func TestGitLogSuccess(t *testing.T) {
	fn, calls := mockExec("abc123 feat: add thing\ndef456 fix: bug\n", nil)
	tool := chips.NewGitLogTool(fn, "")

	input := mustJSON(t, map[string]any{"count": 5})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "abc123") {
		t.Errorf("unexpected output: %q", out)
	}
	if !strings.Contains((*calls)[0], "-n5") {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

func TestGitLogDefaultCount(t *testing.T) {
	fn, calls := mockExec("ok\n", nil)
	tool := chips.NewGitLogTool(fn, "")

	input := mustJSON(t, map[string]any{})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains((*calls)[0], "-n20") {
		t.Errorf("expected default count 20, got: %q", (*calls)[0])
	}
}

func TestGitLogClampedCount(t *testing.T) {
	fn, calls := mockExec("ok\n", nil)
	tool := chips.NewGitLogTool(fn, "")

	input := mustJSON(t, map[string]any{"count": 500})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains((*calls)[0], "-n100") {
		t.Errorf("expected clamped count 100, got: %q", (*calls)[0])
	}
}

func TestGitLogRefFlagInjection(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := chips.NewGitLogTool(fn, "")

	input := mustJSON(t, map[string]any{"ref": "--all"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for ref starting with '-'")
	}
}

// --- GitDiffTool tests ---

func TestGitDiffSuccess(t *testing.T) {
	fn, calls := mockExec("diff --git a/file b/file\n+added\n", nil)
	tool := chips.NewGitDiffTool(fn, "")

	input := mustJSON(t, map[string]any{"ref1": "main", "ref2": "feat/branch"})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "+added") {
		t.Errorf("unexpected output: %q", out)
	}
	if !strings.Contains((*calls)[0], "main..feat/branch") {
		t.Errorf("unexpected command: %q", (*calls)[0])
	}
}

func TestGitDiffDefaultRef2(t *testing.T) {
	fn, calls := mockExec("ok\n", nil)
	tool := chips.NewGitDiffTool(fn, "")

	input := mustJSON(t, map[string]any{"ref1": "main"})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains((*calls)[0], "main..HEAD") {
		t.Errorf("expected default HEAD, got: %q", (*calls)[0])
	}
}

func TestGitDiffEmptyRef1(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := chips.NewGitDiffTool(fn, "")

	input := mustJSON(t, map[string]any{"ref1": ""})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for empty ref1")
	}
}

func TestGitDiffRefFlagInjection(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := chips.NewGitDiffTool(fn, "")

	input := mustJSON(t, map[string]any{"ref1": "--raw"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for ref1 starting with '-'")
	}
}

func TestGitDiffRef2FlagInjection(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := chips.NewGitDiffTool(fn, "")

	input := mustJSON(t, map[string]any{"ref1": "main", "ref2": "--stat"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for ref2 starting with '-'")
	}
}

func TestGitDiffExecError(t *testing.T) {
	fn, _ := mockExec("", fmt.Errorf("fatal: bad ref"))
	tool := chips.NewGitDiffTool(fn, "")

	input := mustJSON(t, map[string]any{"ref1": "nonexistent"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error from exec failure")
	}
}

// --- Interface coverage for all tools ---

func TestAllToolInterfaces(t *testing.T) {
	fn, _ := mockExec("", nil)
	allTools := []struct {
		name string
		tool interface {
			Name() string
			Description() string
		}
	}{
		{"gh_issue_list", chips.NewGHIssueListTool(fn, testAllowlist, "")},
		{"gh_issue_view", chips.NewGHIssueViewTool(fn, testAllowlist, "")},
		{"gh_pr_list", chips.NewGHPRListTool(fn, testAllowlist, "")},
		{"gh_pr_view", chips.NewGHPRViewTool(fn, testAllowlist, "")},
		{"gh_pr_checks", chips.NewGHPRChecksTool(fn, testAllowlist, "")},
		{"git_log", chips.NewGitLogTool(fn, "")},
		{"git_diff", chips.NewGitDiffTool(fn, "")},
	}
	for _, tc := range allTools {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tool.Name() != tc.name {
				t.Errorf("Name() = %q, want %q", tc.tool.Name(), tc.name)
			}
			if tc.tool.Description() == "" {
				t.Error("Description() should not be empty")
			}
		})
	}
}

// --- Exec error paths ---

func TestGHIssueListExecError(t *testing.T) {
	fn, _ := mockExec("", fmt.Errorf("gh: not found"))
	tool := chips.NewGHIssueListTool(fn, testAllowlist, "")
	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGHIssueViewExecError(t *testing.T) {
	fn, _ := mockExec("", fmt.Errorf("not found"))
	tool := chips.NewGHIssueViewTool(fn, testAllowlist, "")
	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "number": 1})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGHPRListExecError(t *testing.T) {
	fn, _ := mockExec("", fmt.Errorf("gh: error"))
	tool := chips.NewGHPRListTool(fn, testAllowlist, "")
	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge"})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGHPRViewExecError(t *testing.T) {
	fn, _ := mockExec("", fmt.Errorf("not found"))
	tool := chips.NewGHPRViewTool(fn, testAllowlist, "")
	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "number": 1})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGHPRViewInvalidNumber(t *testing.T) {
	fn, _ := mockExec("", nil)
	tool := chips.NewGHPRViewTool(fn, testAllowlist, "")
	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "number": 0})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error for number <= 0")
	}
}

func TestGHPRChecksExecError(t *testing.T) {
	fn, _ := mockExec("", fmt.Errorf("gh: error"))
	tool := chips.NewGHPRChecksTool(fn, testAllowlist, "")
	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge", "number": 1})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGitLogExecError(t *testing.T) {
	fn, _ := mockExec("", fmt.Errorf("fatal: bad ref"))
	tool := chips.NewGitLogTool(fn, "")
	input := mustJSON(t, map[string]any{})
	_, err := tool.Execute(t.Context(), input)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGitLogWithRef(t *testing.T) {
	fn, calls := mockExec("abc123 commit\n", nil)
	tool := chips.NewGitLogTool(fn, "")
	input := mustJSON(t, map[string]any{"ref": "feat/branch"})
	_, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains((*calls)[0], "-- feat/branch") {
		t.Errorf("expected ref in command, got: %q", (*calls)[0])
	}
}

// --- Token sanitisation across tools ---

func TestTokenSanitisedInOutput(t *testing.T) {
	token := "ghp_secret123"
	fn, _ := mockExec("output contains ghp_secret123 token", nil)
	tool := chips.NewGHIssueListTool(fn, testAllowlist, token)

	input := mustJSON(t, map[string]any{"org": "klazomenai", "repo": "bridge"})
	out, err := tool.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out, token) {
		t.Error("output should not contain the token")
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Error("expected [REDACTED] in output")
	}
}

// --- DefaultExecFnWithToken end-to-end env injection ---
//
// These tests cover the production path established by bridge#141 +
// the Copilot review on PR #174. The pure-function gating + filter
// logic is unit-tested in exec_internal_test.go via buildGhChildEnv
// directly; the tests below verify end-to-end subprocess behaviour
// using a temp `gh` stub script, which exercises the path where
// Go's exec.CommandContext actually applies Cmd.Env to the child.

// makeGhStub writes a shell script named `gh` into a fresh temp
// directory and returns its full path. The script prints the
// child's gh-auth env vars in a stable format so tests can assert
// on exact content. Used to verify DefaultExecFnWithToken's
// filepath.Base("gh") gating end-to-end without needing the real
// gh CLI in the test environment.
func makeGhStub(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no /bin/sh on this host — skipping subprocess test")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "gh")
	script := []byte(`#!/bin/sh
printf "GH_TOKEN=%s|GITHUB_TOKEN=%s" "$GH_TOKEN" "$GITHUB_TOKEN"
`)
	if err := os.WriteFile(stub, script, 0o755); err != nil {
		t.Fatalf("write gh stub: %v", err)
	}
	return stub
}

func TestDefaultExecFnWithToken_InjectsForGhCommand_EndToEnd(t *testing.T) {
	// Single end-to-end subprocess test that proves the helper
	// (a) gates on filepath.Base("gh"), (b) filters parent's stray
	// GH_TOKEN/GITHUB_TOKEN, and (c) the injected value reaches the
	// child via Cmd.Env. The gh stub is invoked by its full path
	// (/tmp/.../gh) so the test also covers basename-of-a-path
	// resolution.
	stub := makeGhStub(t)
	t.Setenv("GH_TOKEN", "stray_should_be_filtered")
	t.Setenv("GITHUB_TOKEN", "also_stray")

	fn := chips.DefaultExecFnWithToken("ghp_authoritative_value")
	out, err := fn(t.Context(), stub)
	if err != nil {
		t.Fatalf("exec gh stub: %v", err)
	}
	want := "GH_TOKEN=|GITHUB_TOKEN=ghp_authoritative_value"
	if string(out) != want {
		t.Errorf("child env mismatch:\n  got:  %q\n  want: %q", out, want)
	}
}

func TestDefaultExecFnWithToken_StripsGhAuthFromNonGhCommand(t *testing.T) {
	// `git log` / `git diff` and any other non-gh subprocess must NOT
	// receive GH_TOKEN or GITHUB_TOKEN even when those vars are present
	// in the bridge's own os.Environ() (e.g. dev shell, or
	// --insecure-token-from-env mode). The parent auth vars are
	// stripped from ALL subprocess envs; the loaded token is injected
	// only into gh subprocesses. Verified by setting a sentinel
	// GITHUB_TOKEN in the parent and asserting it does NOT appear in
	// the sh child's environment.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no /bin/sh on this host")
	}
	t.Setenv("GITHUB_TOKEN", "parent_value_must_be_stripped_for_non_gh")

	fn := chips.DefaultExecFnWithToken("ghp_not_for_sh_either")
	out, err := fn(t.Context(), "sh", "-c", `printf "%s" "$GITHUB_TOKEN"`)
	if err != nil {
		t.Fatalf("exec sh: %v", err)
	}
	if string(out) != "" {
		t.Errorf("non-gh command received GITHUB_TOKEN from parent: %q (should be stripped)", out)
	}
}

func TestDefaultExecFnWithToken_DoesNotPolluteParentEnv(t *testing.T) {
	// The whole point of the file-mount path is that the bridge's
	// own os.Environ() does NOT contain GITHUB_TOKEN. Even when
	// the helper IS configured to inject (token non-empty + gh
	// command name), it must inject into the child's Cmd.Env only —
	// never os.Setenv into the parent. Snapshot before+after.
	stub := makeGhStub(t)
	t.Setenv("GITHUB_TOKEN", "")
	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		t.Fatalf("test setup: GITHUB_TOKEN should be empty pre-call, got %q", v)
	}

	fn := chips.DefaultExecFnWithToken("ghp_must_not_leak_into_parent")
	if _, err := fn(t.Context(), stub); err != nil {
		t.Fatalf("exec gh stub: %v", err)
	}

	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		t.Errorf("parent os.Environ() polluted by DefaultExecFnWithToken: GITHUB_TOKEN=%q", v)
	}
}

func TestDefaultExecFnWithToken_EmptyTokenStillStripsParentGhAuth(t *testing.T) {
	// Empty token means no injection, but GH_TOKEN / GITHUB_TOKEN
	// present in the parent env must still be stripped from the
	// subprocess — even for a gh command. The "empty token" path
	// corresponds to chips.enabled=false (no secret configured);
	// real gh calls don't run in that state (stub tools fire
	// instead), but the subprocess env contract holds regardless.
	stub := makeGhStub(t)
	t.Setenv("GITHUB_TOKEN", "parent_must_be_stripped_for_empty_token")

	fn := chips.DefaultExecFnWithToken("")
	out, err := fn(t.Context(), stub)
	if err != nil {
		t.Fatalf("exec gh stub: %v", err)
	}
	// Stub prints GH_TOKEN=$GH_TOKEN|GITHUB_TOKEN=$GITHUB_TOKEN;
	// both must be empty — parent auth stripped, nothing injected.
	if string(out) != "GH_TOKEN=|GITHUB_TOKEN=" {
		t.Errorf("empty token: parent GH auth leaked:\n  got:  %q\n  want: %q", out, "GH_TOKEN=|GITHUB_TOKEN=")
	}
}


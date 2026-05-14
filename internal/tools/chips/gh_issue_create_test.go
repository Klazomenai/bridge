package chips_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	chips "klazomenai/bridge/internal/tools/chips"
)

func TestGHIssueCreateTool_HappyPath(t *testing.T) {
	const issueURL = "https://github.com/klazomenai/bridge/issues/999"
	exec := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "gh" {
			t.Errorf("expected gh, got %q", name)
		}
		return []byte(issueURL + "\n"), nil
	}
	tool := chips.NewGHIssueCreateTool(exec, chips.ParseRepoAllowlist("klazomenai/bridge"), "test-token")

	out, err := tool.Execute(t.Context(), json.RawMessage(`{
		"org": "klazomenai", "repo": "bridge",
		"title": "Test issue", "body": "Test body"
	}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "klazomenai/bridge/issues/") {
		t.Errorf("output does not contain issue URL: %q", out)
	}
}

func TestGHIssueCreateTool_HappyPath_WithLabelsAndAssignees(t *testing.T) {
	var capturedArgs []string
	exec := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte("https://github.com/klazomenai/bridge/issues/1000\n"), nil
	}
	tool := chips.NewGHIssueCreateTool(exec, chips.ParseRepoAllowlist("klazomenai/bridge"), "test-token")

	_, err := tool.Execute(t.Context(), json.RawMessage(`{
		"org": "klazomenai", "repo": "bridge",
		"title": "Labelled issue", "body": "body",
		"labels": ["bug", "enhancement"],
		"assignees": ["Klazomenai"]
	}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Verify --label and --assignee flags were passed for each entry.
	args := strings.Join(capturedArgs, " ")
	for _, want := range []string{"--label bug", "--label enhancement", "--assignee Klazomenai"} {
		if !strings.Contains(args, want) {
			t.Errorf("expected %q in args %q", want, args)
		}
	}
}

func TestGHIssueCreateTool_AllowlistRefusal(t *testing.T) {
	exec := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		t.Fatal("exec called — allowlist should have refused before exec")
		return nil, nil
	}
	tool := chips.NewGHIssueCreateTool(exec, chips.ParseRepoAllowlist("klazomenai/bridge"), "test-token")

	_, err := tool.Execute(t.Context(), json.RawMessage(`{
		"org": "microsoft", "repo": "vscode",
		"title": "Infiltration issue", "body": "body"
	}`))
	if err == nil {
		t.Fatal("expected allowlist refusal error, got nil")
	}
	if !strings.Contains(err.Error(), "not in the allowed list") {
		t.Errorf("expected 'not in the allowed list' in error, got: %v", err)
	}
}

func TestGHIssueCreateTool_InvalidInput_MissingTitle(t *testing.T) {
	exec := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		t.Fatal("exec called — validation should have failed before exec")
		return nil, nil
	}
	tool := chips.NewGHIssueCreateTool(exec, chips.ParseRepoAllowlist("klazomenai/bridge"), "test-token")

	_, err := tool.Execute(t.Context(), json.RawMessage(`{
		"org": "klazomenai", "repo": "bridge",
		"title": "", "body": "body"
	}`))
	if err == nil {
		t.Fatal("expected error for empty title, got nil")
	}
}

func TestGHIssueCreateTool_InvalidInput_MalformedJSON(t *testing.T) {
	tool := chips.NewGHIssueCreateTool(
		func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil },
		chips.ParseRepoAllowlist("klazomenai/bridge"),
		"test-token",
	)
	_, err := tool.Execute(t.Context(), json.RawMessage(`{not valid json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestGHIssueCreateTool_ExecError_Surfaced(t *testing.T) {
	exec := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("gh: authentication required")
	}
	tool := chips.NewGHIssueCreateTool(exec, chips.ParseRepoAllowlist("klazomenai/bridge"), "test-token")

	_, err := tool.Execute(t.Context(), json.RawMessage(`{
		"org": "klazomenai", "repo": "bridge",
		"title": "Test", "body": "body"
	}`))
	if err == nil {
		t.Fatal("expected exec error to surface, got nil")
	}
	if !strings.Contains(err.Error(), "gh issue create") {
		t.Errorf("expected 'gh issue create' context in error, got: %v", err)
	}
}

func TestGHIssueCreateTool_TokenRedactedInOutput(t *testing.T) {
	// If the token somehow appears in gh's stdout (e.g. an error message
	// echoing the auth header), sanitiseOutput must strip it before
	// returning to the orchestrator. Verify via SanitiseOutputForTest
	// which exposes the same chain as Execute.
	const token = "ghp_test_redact_token_12345678901"
	out := chips.SanitiseOutputForTest(
		"authenticated as "+token+" on klazomenai/bridge",
		token,
		"gh_issue_create",
	)
	if strings.Contains(out, token) {
		t.Errorf("token not redacted in sanitised output: %q", out)
	}
}

func TestGHIssueCreateTool_MutationTrue(t *testing.T) {
	// GHIssueCreateTool must implement MutationAware returning true so
	// the orchestrator marks audit records with mutation:true.
	tool := chips.NewGHIssueCreateTool(
		func(_ context.Context, _ string, _ ...string) ([]byte, error) { return nil, nil },
		chips.ParseRepoAllowlist("klazomenai/bridge"),
		"test-token",
	)
	if !tool.Mutation() {
		t.Error("GHIssueCreateTool.Mutation() returned false; expected true")
	}
}

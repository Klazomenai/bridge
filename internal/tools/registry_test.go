package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"klazomenai/bridge/internal/tools"
	chipstools "klazomenai/bridge/internal/tools/chips"
)

// stubTool is a minimal ToolDefinition for testing.
type stubTool struct {
	name string
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return "stub: " + s.name }
func (s *stubTool) InputSchema() anthropic.ToolInputSchemaParam {
	return anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"query": map[string]string{"type": "string"},
		},
	}
}
func (s *stubTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "ok", nil
}

func TestRegisterAndGet(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "alpha"})

	if tool := reg.Get("alpha"); tool == nil {
		t.Fatal("expected tool 'alpha' to be registered")
	}
	if tool := reg.Get("beta"); tool != nil {
		t.Fatal("expected nil for unregistered tool")
	}
}

func TestHas(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "alpha"})

	if !reg.Has("alpha") {
		t.Fatal("expected Has('alpha') to be true")
	}
	if reg.Has("beta") {
		t.Fatal("expected Has('beta') to be false")
	}
}

func TestRegisterNilToolPanics(t *testing.T) {
	reg := tools.NewRegistry()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil tool registration")
		}
	}()
	reg.Register(nil)
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	reg := tools.NewRegistry()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty name registration")
		}
	}()
	reg.Register(&stubTool{name: ""})
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "alpha"})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	reg.Register(&stubTool{name: "alpha"})
}

func TestForCrew(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "alpha"})
	reg.Register(&stubTool{name: "beta"})
	reg.Register(&stubTool{name: "gamma"})

	// Crew declares alpha and gamma — should get 2 params.
	params := reg.ForCrew([]string{"alpha", "gamma"})
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(params))
	}
	if params[0].OfTool.Name != "alpha" {
		t.Errorf("expected first tool 'alpha', got %q", params[0].OfTool.Name)
	}
	if params[1].OfTool.Name != "gamma" {
		t.Errorf("expected second tool 'gamma', got %q", params[1].OfTool.Name)
	}
}

func TestForCrewSkipsUnknown(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "alpha"})

	params := reg.ForCrew([]string{"alpha", "nonexistent"})
	if len(params) != 1 {
		t.Fatalf("expected 1 param (unknown skipped), got %d", len(params))
	}
}

func TestForCrewEmptyTools(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "alpha"})

	params := reg.ForCrew([]string{})
	if len(params) != 0 {
		t.Fatalf("expected 0 params for empty tools list, got %d", len(params))
	}

	params = reg.ForCrew(nil)
	if len(params) != 0 {
		t.Fatalf("expected 0 params for nil tools list, got %d", len(params))
	}
}

func TestExecuteKnownTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "alpha"})

	result, err := reg.Execute(t.Context(), "alpha", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

func TestExecuteUnknownToolReturnsError(t *testing.T) {
	reg := tools.NewRegistry()

	_, err := reg.Execute(t.Context(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if err.Error() != `unknown tool: "nonexistent"` {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNames(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "alpha"})
	reg.Register(&stubTool{name: "beta"})

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["alpha"] || !nameSet["beta"] {
		t.Errorf("expected alpha and beta in names, got %v", names)
	}
}

// mutatingTool implements MutationAware returning a configurable bool.
// Used by TestIsMutation to exercise both branches of the helper.
type mutatingTool struct {
	stubTool
	mutates bool
}

func (m *mutatingTool) Mutation() bool { return m.mutates }

func TestIsMutation(t *testing.T) {
	cases := []struct {
		name string
		tool tools.ToolDefinition
		want bool
	}{
		{
			name: "tool implementing MutationAware true",
			tool: &mutatingTool{stubTool: stubTool{name: "writer"}, mutates: true},
			want: true,
		},
		{
			name: "tool implementing MutationAware false",
			tool: &mutatingTool{stubTool: stubTool{name: "reader"}, mutates: false},
			want: false,
		},
		{
			name: "tool NOT implementing MutationAware (conservative default)",
			tool: &stubTool{name: "legacy"},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tools.IsMutation(tc.tool); got != tc.want {
				t.Errorf("IsMutation(%T) = %v, want %v", tc.tool, got, tc.want)
			}
		})
	}
}

// TestGhPrMergeNotRegistered is one of the L2 enforcement tests on #154's AC.
// Builds the production chips registry via the real RegisterChipsTools
// entrypoint and asserts the high-risk mutations gh_pr_merge and
// gh_pr_ready are not registered. Those tools are reserved as human
// decisions per the operator's standing rule; _universal.md requires
// they not be callable at all rather than gated at execute time (which
// would be bypassable via prompt injection).
//
// Lives in internal/tools/registry_test.go per AC; uses the external
// tools_test package so importing chipstools forms only a one-way
// dependency (tools_test → chipstools → tools), no cycle.
//
// Companion tests in other packages cover the prompt-content rule
// (internal/crew) and runtime allowlist refusal (internal/orchestrator).
func TestGhPrMergeNotRegistered(t *testing.T) {
	reg := tools.NewRegistry()
	chipstools.RegisterChipsTools(
		reg,
		chipstools.DefaultExecFn(),
		chipstools.ParseRepoAllowlist("klazomenai/bridge"),
		"test-token",
	)
	for _, forbidden := range []string{"gh_pr_merge", "gh_pr_ready"} {
		if reg.Has(forbidden) {
			t.Errorf("production chips registry has %q — high-risk mutation must not be registered", forbidden)
		}
	}
	// Anchor the roster's positive shape so a future deletion of
	// RegisterChipsTools wouldn't silently turn this into a vacuous test.
	for _, expected := range []string{
		"gh_issue_list", "gh_issue_view", "gh_issue_create",
		"gh_pr_list", "gh_pr_view", "gh_pr_checks", "git_log", "git_diff",
	} {
		if !reg.Has(expected) {
			t.Errorf("production chips registry missing expected tool %q", expected)
		}
	}
}

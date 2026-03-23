package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"klazomenai/bridge/internal/tools"
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

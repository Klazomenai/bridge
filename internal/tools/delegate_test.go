package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"klazomenai/bridge/internal/tools"
)

func TestDelegateToolHappyPath(t *testing.T) {
	d := &tools.DelegateTool{}
	input, _ := json.Marshal(map[string]string{"crew": "crest", "context": "Check the inbox"})

	result, err := d.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, tools.DelegateSentinel) {
		t.Errorf("expected sentinel prefix, got %q", result)
	}

	crewID, ctx, ok := tools.ParseDelegation(result)
	if !ok {
		t.Fatal("ParseDelegation returned false")
	}
	if crewID != "crest" {
		t.Errorf("crew = %q, want crest", crewID)
	}
	if ctx != "Check the inbox" {
		t.Errorf("context = %q, want 'Check the inbox'", ctx)
	}
}

func TestDelegateToolMissingCrew(t *testing.T) {
	d := &tools.DelegateTool{}
	input, _ := json.Marshal(map[string]string{"context": "Check the inbox"})

	_, err := d.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for missing crew")
	}
	if !strings.Contains(err.Error(), "crew is required") {
		t.Errorf("error = %q, want 'crew is required'", err.Error())
	}
}

func TestDelegateToolMissingContext(t *testing.T) {
	d := &tools.DelegateTool{}
	input, _ := json.Marshal(map[string]string{"crew": "crest"})

	_, err := d.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for missing context")
	}
	if !strings.Contains(err.Error(), "context is required") {
		t.Errorf("error = %q, want 'context is required'", err.Error())
	}
}

func TestDelegateToolNormalisesInput(t *testing.T) {
	d := &tools.DelegateTool{}
	input, _ := json.Marshal(map[string]string{"crew": "  CREST  ", "context": "  Check inbox  "})

	result, err := d.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	crewID, ctx, ok := tools.ParseDelegation(result)
	if !ok {
		t.Fatal("ParseDelegation returned false")
	}
	if crewID != "crest" {
		t.Errorf("crew = %q, want crest (lowercased, trimmed)", crewID)
	}
	if ctx != "Check inbox" {
		t.Errorf("context = %q, want 'Check inbox' (trimmed)", ctx)
	}
}

func TestDelegateToolRejectsColonInCrew(t *testing.T) {
	d := &tools.DelegateTool{}
	input, _ := json.Marshal(map[string]string{"crew": "crest:evil", "context": "test"})

	_, err := d.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for colon in crew ID")
	}
	if !strings.Contains(err.Error(), "must not contain ':'") {
		t.Errorf("error = %q, want colon rejection", err.Error())
	}
}

func TestParseDelegationNotSentinel(t *testing.T) {
	_, _, ok := tools.ParseDelegation("just a normal result")
	if ok {
		t.Error("expected false for non-sentinel string")
	}
}

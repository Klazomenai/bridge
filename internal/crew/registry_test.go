package crew_test

import (
	"os"
	"path/filepath"
	"testing"

	"klazomenai/bridge/internal/crew"
)

const validYAML = `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
  crest:
    name: "Crest"
    role: "Signalman"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      model: "en_US-lessac-high.onnx"
      announces_as: "Crest:"
    system_prompt: "You are Crest. Respond in {verbosity}"
`

func writeRegistry(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "crew.yaml")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp registry: %v", err)
	}
	return f
}

func TestLoadNonExistentFileReturnsError(t *testing.T) {
	_, err := crew.Load("/tmp/does-not-exist-crew.yaml")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestLoadValidYAML(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestCrewLookupReturnsCorrectInstance(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	maren := r.Get("maren")
	if maren == nil {
		t.Fatal("expected to find maren")
	}
	if maren.Name() != "Maren" {
		t.Errorf("got name %q, want Maren", maren.Name())
	}
	if maren.ID() != "maren" {
		t.Errorf("got id %q, want maren", maren.ID())
	}
}

func TestDefaultCrewExistsInRegistry(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	def := r.Default()
	if def == nil {
		t.Fatal("expected default crew member")
	}
	if def.ID() != r.DefaultID() {
		t.Errorf("default crew ID mismatch: got %q, want %q", def.ID(), r.DefaultID())
	}
}

func TestLoadMissingRequiredFieldErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "missing default_crew",
			yaml: `
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      model: "x.onnx"
      announces_as: "Maren:"
    system_prompt: "prompt"
`,
		},
		{
			name: "default_crew not in crew list",
			yaml: `
default_crew: ghost
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      model: "x.onnx"
      announces_as: "Maren:"
    system_prompt: "prompt"
`,
		},
		{
			name: "missing model",
			yaml: `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    verbosity: dispatch
    voice:
      model: "x.onnx"
      announces_as: "Maren:"
    system_prompt: "prompt"
`,
		},
		{
			name: "missing name",
			yaml: `
default_crew: maren
crew:
  maren:
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      model: "x.onnx"
      announces_as: "Maren:"
    system_prompt: "prompt"
`,
		},
		{
			name: "missing role",
			yaml: `
default_crew: maren
crew:
  maren:
    name: "Maren"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      model: "x.onnx"
      announces_as: "Maren:"
    system_prompt: "prompt"
`,
		},
		{
			name: "missing verbosity",
			yaml: `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    voice:
      model: "x.onnx"
      announces_as: "Maren:"
    system_prompt: "prompt"
`,
		},
		{
			name: "missing system_prompt",
			yaml: `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      model: "x.onnx"
      announces_as: "Maren:"
`,
		},
		{
			name: "unknown verbosity",
			yaml: `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: turbo
    voice:
      model: "x.onnx"
      announces_as: "Maren:"
    system_prompt: "prompt"
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeRegistry(t, tc.yaml)
			_, err := crew.Load(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestCrewGetters(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	maren := r.Get("maren")
	if maren == nil {
		t.Fatal("maren not found")
	}
	// Exercise all getters to improve coverage.
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"ID", maren.ID(), "maren"},
		{"Name", maren.Name(), "Maren"},
		{"Role", maren.Role(), "Shipwright"},
		{"Model", maren.Model(), "claude-sonnet-4-6"},
		{"Verbosity", maren.Verbosity(), "dispatch"},
		{"AnnouncesAs", maren.AnnouncesAs(), "Maren:"},
		{"VoiceModel", maren.VoiceModel(), "en_GB-cori-high.onnx"},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("got %q, want %q", c.got, c.want)
			}
		})
	}
}

func TestVerbosityInjectedInSystemPrompt(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	maren := r.Get("maren")
	if maren == nil {
		t.Fatal("maren not found")
	}
	// {verbosity} should be replaced with the dispatch description.
	if maren.SystemPrompt() == "" {
		t.Fatal("system prompt is empty")
	}
	// The literal placeholder must not appear in the rendered prompt.
	for _, ph := range []string{"{verbosity}"} {
		if contains := func(s, sub string) bool {
			return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
		}(maren.SystemPrompt(), ph); contains {
			t.Errorf("system prompt still contains unresolved placeholder %q", ph)
		}
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

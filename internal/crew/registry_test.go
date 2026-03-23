package crew_test

import (
	"os"
	"path/filepath"
	"strings"
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
			name: "missing voice.model",
			yaml: `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    voice:
      announces_as: "Maren:"
    system_prompt: "prompt {verbosity}"
`,
		},
		{
			name: "missing voice.announces_as",
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
    system_prompt: "prompt {verbosity}"
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
		{
			name: "uppercase crew ID",
			yaml: `
default_crew: Maren
crew:
  Maren:
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

func TestRegistryIDs(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ids := r.IDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 crew IDs, got %d: %v", len(ids), ids)
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		seen[id] = true
	}
	for _, want := range []string{"maren", "crest"} {
		if !seen[want] {
			t.Errorf("expected ID %q in IDs(), got %v", want, ids)
		}
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
	if strings.Contains(maren.SystemPrompt(), "{verbosity}") {
		t.Errorf("system prompt still contains unresolved placeholder {verbosity}")
	}
}

const yamlWithTools = `
default_crew: maren
crew:
  maren:
    name: "Maren"
    role: "Shipwright"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [kubectl_get, helm_status]
    voice:
      model: "en_GB-cori-high.onnx"
      announces_as: "Maren:"
    system_prompt: "You are Maren. Respond in {verbosity}"
  crest:
    name: "Crest"
    role: "Signalman"
    model: "claude-sonnet-4-6"
    verbosity: dispatch
    tools: [imap_poll]
    voice:
      model: "en_US-lessac-high.onnx"
      announces_as: "Crest:"
    system_prompt: "You are Crest. Respond in {verbosity}"
`

func TestToolsParsedFromYAML(t *testing.T) {
	path := writeRegistry(t, yamlWithTools)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	maren := r.Get("maren")
	if len(maren.Tools()) != 2 {
		t.Fatalf("expected 2 tools for maren, got %d", len(maren.Tools()))
	}
	if maren.Tools()[0] != "kubectl_get" || maren.Tools()[1] != "helm_status" {
		t.Errorf("unexpected tools: %v", maren.Tools())
	}

	crest := r.Get("crest")
	if len(crest.Tools()) != 1 || crest.Tools()[0] != "imap_poll" {
		t.Errorf("unexpected tools for crest: %v", crest.Tools())
	}
}

func TestToolsEmptyWhenOmitted(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	maren := r.Get("maren")
	if len(maren.Tools()) != 0 {
		t.Errorf("expected empty tools, got %v", maren.Tools())
	}
}

// stubChecker implements crew.ToolChecker for testing.
type stubChecker struct {
	known map[string]bool
}

func (s *stubChecker) Has(name string) bool { return s.known[name] }

func TestValidateToolsAllPresent(t *testing.T) {
	path := writeRegistry(t, yamlWithTools)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	checker := &stubChecker{known: map[string]bool{
		"kubectl_get": true,
		"helm_status": true,
		"imap_poll":   true,
	}}
	if err := r.ValidateTools(checker); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateToolsMissing(t *testing.T) {
	path := writeRegistry(t, yamlWithTools)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Only kubectl_get is known — helm_status and imap_poll are missing.
	checker := &stubChecker{known: map[string]bool{
		"kubectl_get": true,
	}}
	err = r.ValidateTools(checker)
	if err == nil {
		t.Fatal("expected validation error for missing tools")
	}
	if !strings.Contains(err.Error(), "helm_status") {
		t.Errorf("expected error to mention helm_status: %v", err)
	}
	if !strings.Contains(err.Error(), "imap_poll") {
		t.Errorf("expected error to mention imap_poll: %v", err)
	}
}

func TestValidateToolsNoToolsDeclared(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Empty checker — no tools registered, but no tools declared either.
	checker := &stubChecker{known: map[string]bool{}}
	if err := r.ValidateTools(checker); err != nil {
		t.Fatalf("expected no error when no tools declared, got: %v", err)
	}
}

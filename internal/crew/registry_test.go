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
  bosun:
    name: "Bosun"
    role: "Deck Supervisor"
    model: "claude-sonnet-4-6"
    verbosity: log-entry
    voice:
      model: "en_GB-alan-medium.onnx"
      announces_as: "Bosun:"
    system_prompt: "You are Bosun. Respond in {verbosity}"
  lookout:
    name: "Lookout"
    role: "Watch"
    model: "claude-haiku-4-5"
    verbosity: signal
    voice:
      model: "en_US-amy-medium.onnx"
      announces_as: "Lookout:"
    system_prompt: "You are the Lookout. Respond in {verbosity}"
  chips:
    name: "Chips"
    role: "The Carpenter"
    model: "claude-sonnet-4-6"
    verbosity: log-entry
    voice:
      model: TBD
      announces_as: "Chips:"
    system_prompt: "You are Chips. Respond in {verbosity}"
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
	if len(ids) != 5 {
		t.Fatalf("expected 5 crew IDs, got %d: %v", len(ids), ids)
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		seen[id] = true
	}
	for _, want := range []string{"maren", "crest", "bosun", "lookout", "chips"} {
		if !seen[want] {
			t.Errorf("expected ID %q in IDs(), got %v", want, ids)
		}
	}
}

func TestBosunAndLookoutGetters(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	bosun := r.Get("bosun")
	if bosun == nil {
		t.Fatal("bosun not found")
	}
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"ID", bosun.ID(), "bosun"},
		{"Name", bosun.Name(), "Bosun"},
		{"Role", bosun.Role(), "Deck Supervisor"},
		{"Model", bosun.Model(), "claude-sonnet-4-6"},
		{"Verbosity", bosun.Verbosity(), "log-entry"},
		{"AnnouncesAs", bosun.AnnouncesAs(), "Bosun:"},
		{"VoiceModel", bosun.VoiceModel(), "en_GB-alan-medium.onnx"},
	}
	for _, c := range checks {
		t.Run("Bosun/"+c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("got %q, want %q", c.got, c.want)
			}
		})
	}

	lookout := r.Get("lookout")
	if lookout == nil {
		t.Fatal("lookout not found")
	}
	lookoutChecks := []struct {
		name string
		got  string
		want string
	}{
		{"ID", lookout.ID(), "lookout"},
		{"Name", lookout.Name(), "Lookout"},
		{"Role", lookout.Role(), "Watch"},
		{"Model", lookout.Model(), "claude-haiku-4-5"},
		{"Verbosity", lookout.Verbosity(), "signal"},
		{"AnnouncesAs", lookout.AnnouncesAs(), "Lookout:"},
		{"VoiceModel", lookout.VoiceModel(), "en_US-amy-medium.onnx"},
	}
	for _, c := range lookoutChecks {
		t.Run("Lookout/"+c.name, func(t *testing.T) {
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
  bosun:
    name: "Bosun"
    role: "Deck Supervisor"
    model: "claude-sonnet-4-6"
    verbosity: log-entry
    tools: [kubectl_get]
    voice:
      model: "en_GB-alan-medium.onnx"
      announces_as: "Bosun:"
    system_prompt: "You are Bosun. Respond in {verbosity}"
  lookout:
    name: "Lookout"
    role: "Watch"
    model: "claude-haiku-4-5"
    verbosity: signal
    tools: [prometheus_query, loki_query]
    voice:
      model: "en_US-amy-medium.onnx"
      announces_as: "Lookout:"
    system_prompt: "You are the Lookout. Respond in {verbosity}"
  chips:
    name: "Chips"
    role: "The Carpenter"
    model: "claude-sonnet-4-6"
    verbosity: log-entry
    tools: [gh_issue_list, gh_issue_view, gh_pr_list, gh_pr_view, gh_pr_checks, git_log, git_diff]
    voice:
      model: TBD
      announces_as: "Chips:"
    system_prompt: "You are Chips. Respond in {verbosity}"
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
		"kubectl_get":      true,
		"helm_status":      true,
		"imap_poll":        true,
		"prometheus_query": true,
		"loki_query":       true,
		"gh_issue_list":    true,
		"gh_issue_view":    true,
		"gh_pr_list":       true,
		"gh_pr_view":       true,
		"gh_pr_checks":     true,
		"git_log":          true,
		"git_diff":         true,
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

// TestRealCrewYAMLLoadsAndValidates loads the repository's config/crew.yaml
// and validates all declared tools against the known registration set. This
// catches config/registration drift that synthetic test YAML would miss.
func TestRealCrewYAMLLoadsAndValidates(t *testing.T) {
	const configPath = "../../config/crew.yaml"
	if _, err := os.Stat(configPath); err != nil {
		t.Skipf("config/crew.yaml not found at %s (running outside repo?)", configPath)
	}

	r, err := crew.Load(configPath)
	if err != nil {
		t.Fatalf("Load real crew.yaml: %v", err)
	}

	// All tool names that main.go registers (real or stub).
	allTools := &stubChecker{known: map[string]bool{
		"delegate_to_crew":  true,
		"imap_poll":         true,
		"smtp_send":         true,
		"kubectl_get":       true,
		"helm_status":       true,
		"prometheus_query":  true,
		"loki_query":        true,
		"gh_issue_list":     true,
		"gh_issue_view":     true,
		"gh_pr_list":        true,
		"gh_pr_view":        true,
		"gh_pr_checks":      true,
		"git_log":           true,
		"git_diff":          true,
	}}
	if err := r.ValidateTools(allTools); err != nil {
		t.Fatalf("real crew.yaml tool validation failed: %v", err)
	}
}

// TestChipsSystemPromptContainsGitHubSkill verifies that the embedded github
// skill body is appended to Chips' system prompt and that key rules survive
// the build pipeline. Loads the real config/crew.yaml so a divergence between
// the YAML prompt and the embedded skill is caught.
func TestChipsSystemPromptContainsGitHubSkill(t *testing.T) {
	const configPath = "../../config/crew.yaml"
	if _, err := os.Stat(configPath); err != nil {
		t.Skipf("config/crew.yaml not found at %s (running outside repo?)", configPath)
	}

	r, err := crew.Load(configPath)
	if err != nil {
		t.Fatalf("Load real crew.yaml: %v", err)
	}

	chips := r.Get("chips")
	if chips == nil {
		t.Fatal("chips not found in real crew.yaml")
	}

	prompt := chips.SystemPrompt()

	// Persona prompt itself must still be there (gate did not replace it).
	if !strings.Contains(prompt, "Carpenter") {
		t.Error("chips persona prompt missing — embedding overwrote rather than appended")
	}

	// Key skill rules that must survive build-time embedding. Each entry
	// is a load-bearing rule; failure means the skill body was truncated,
	// the gating logic dropped chips, or the source skill drifted away
	// from the operator's standing instructions.
	//
	// TODO(#148): migrate to compose_test.go after Source/Compose lands.
	// Once the skills loader rewrite is in, this assertion table moves
	// into the new compose_test.go alongside the universal+profile
	// composition tests. The fragments here describe the OLD vendored
	// monolithic skill body (post-PR144); the new layout will assert
	// against the universal+skill+profile concatenation. Keep the
	// fragments as a regression net during the migration.
	requiredRules := []struct {
		name    string
		fragment string
	}{
		{"signed commits", "--gpg-sign"},
		{"refs not closes", "Refs #N"},
		{"closes forbidden", "NEVER `Closes #N`"},
		{"draft default", "ALWAYS create PRs as draft"},
		{"never push main", "NEVER push to `main`"},
		{"never amend", "NEVER amend commits"},
		{"no auto-merge", "NEVER run `gh pr merge`"},
		{"conventional commits", "Conventional commits format"},
		{"end-of-title emoji", "Emojis go at the END"},
		{"copilot review workflow", "Copilot Review Workflow"},
	}
	for _, rule := range requiredRules {
		if !strings.Contains(prompt, rule.fragment) {
			t.Errorf("chips prompt missing %s rule: fragment %q not found", rule.name, rule.fragment)
		}
	}

	// Pin the persona ↔ skill boundary so a future edit to crew.yaml's
	// scalar style (e.g. `|` → `|-`, which strips the trailing newline)
	// or a skill-file rewrite that drops the leading header is caught
	// at CI rather than only by visual inspection of the rendered prompt.
	//
	// TODO(#148): migrate to compose_test.go after Source/Compose lands.
	// Under the new loader the boundary marker still applies — Compose
	// emits `\n\n## <Skill> Workflow Rules\n` as the persona↔skill
	// separator. The literal in this assertion will need updating to
	// match Compose's emitted heading; the assertion intent (boundary
	// is stable across YAML chomp style + skill-file edits) carries
	// forward unchanged.
	const boundary = "\n\n## Git + GitHub Workflow Rules"
	if !strings.Contains(prompt, boundary) {
		t.Errorf("chips prompt missing persona↔skill boundary %q — separator may have collapsed", boundary)
	}
}

// TestNonChipsCrewLackGitHubSkill verifies the embedding is gated to chips —
// Maren / Crest / Bosun / Lookout must not inherit GitHub workflow rules
// they have no tools to act on.
func TestNonChipsCrewLackGitHubSkill(t *testing.T) {
	const configPath = "../../config/crew.yaml"
	if _, err := os.Stat(configPath); err != nil {
		t.Skipf("config/crew.yaml not found at %s (running outside repo?)", configPath)
	}

	r, err := crew.Load(configPath)
	if err != nil {
		t.Fatalf("Load real crew.yaml: %v", err)
	}

	// Sentinel fragment unique to the embedded skill body.
	const sentinel = "Copilot Review Workflow"

	for _, id := range []string{"maren", "crest", "bosun", "lookout"} {
		c := r.Get(id)
		if c == nil {
			t.Errorf("%s not found", id)
			continue
		}
		if strings.Contains(c.SystemPrompt(), sentinel) {
			t.Errorf("%s system prompt unexpectedly contains GitHub skill — gating broken", id)
		}
	}
}

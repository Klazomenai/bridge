package crew_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"klazomenai/bridge/internal/crew"
	"klazomenai/bridge/internal/crew/skills"
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
			name: "uppercase default_crew",
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
		{
			// default_crew lowercase passes the early check at the top of
			// LoadWithSource; the uppercase MAP KEY then hits the loop's
			// id != strings.ToLower(id) check inside the for-each. This
			// is a different code path than the "uppercase default_crew"
			// case above (which short-circuits on the prior check).
			name: "uppercase crew map key",
			yaml: `
default_crew: maren
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
		{
			// Hits the yaml.Unmarshal error path (vs. the post-parse
			// validation paths above). Unclosed flow sequence is a clean
			// YAML syntax error that the parser cannot recover from.
			name: "malformed yaml syntax",
			yaml: "default_crew: [unclosed",
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

// TestChipsSystemPromptContainsGitHubSkill verifies that the github skill
// body is composed into Chips' system prompt and that key rules survive
// the build pipeline. Loads the real config/crew.yaml so a divergence
// between the YAML prompt and the embedded skill is caught.
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

	// Persona prompt itself must still be there.
	if !strings.Contains(prompt, "Carpenter") {
		t.Error("chips persona prompt missing — Compose may have replaced rather than prepended")
	}

	// Key skill rules that must survive Compose. Each entry is a
	// load-bearing rule; failure means the skill body was truncated,
	// the source resolution dropped chips, or the source skill drifted
	// away from the operator's standing instructions.
	requiredRules := []struct {
		name     string
		fragment string
	}{
		{"signed commits", "--gpg-sign"},
		{"refs not closes", "Refs #N"},
		{"closes forbidden", "NEVER `Closes #N`"},
		{"draft default", "ALWAYS create PRs as draft"},
		{"never push main", `NEVER push to "main"`},
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
	// scalar style or a skill-file rewrite that drops the leading
	// heading is caught at CI rather than only by visual inspection.
	const boundary = "\n\n## Github Workflow Rules\n"
	if !strings.Contains(prompt, boundary) {
		t.Errorf("chips prompt missing persona↔skill boundary %q — separator may have collapsed", boundary)
	}
}

// TestChipsPromptContainsUniversalRules verifies that the four universal-rule
// sentinels enumerated in #154's AC are composed into Chips's system prompt.
// Loads the real config/crew.yaml so a divergence between the embedded
// skill content and the AC contract is caught at build time.
//
// Sentinels come from two source files (per Compose's output ordering):
//   - "Allowlist is fail-closed", "Operator Intent Required",
//     "Pending-confirmation exception" → embedded/_universal.md
//   - "Refused outright" → embedded/github/profile.md
//
// The test asserts on the full composed prompt regardless of source,
// since the prompt is what's shipped to the model. Plus a worked-example
// stability anchor ("close issue #99") that catches subtle re-wraps in
// the universal-addendum file.
func TestChipsPromptContainsUniversalRules(t *testing.T) {
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
	for _, fragment := range []string{
		"Allowlist is fail-closed",
		"Operator Intent Required",
		"Refused outright",
		"Pending-confirmation exception",
		"close issue #99", // worked-example stability anchor (verbatim from _universal.md)
	} {
		if !strings.Contains(prompt, fragment) {
			t.Errorf("chips prompt missing universal-rule sentinel %q", fragment)
		}
	}
}

// TestChipsPromptContainsGitHubProfileRules verifies that the three github
// profile-addendum sentinels enumerated in #154's AC are composed into
// Chips's system prompt. These rules sit in embedded/github/profile.md
// (the per-skill operator-overlay) and gate Chips's github-specific
// write-surface discipline.
func TestChipsPromptContainsGitHubProfileRules(t *testing.T) {
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
	for _, fragment := range []string{
		"must not be exposed as callable tools",
		"NEVER autonomously resolve Copilot review threads",
		"Refused outright",
	} {
		if !strings.Contains(prompt, fragment) {
			t.Errorf("chips prompt missing github-profile sentinel %q", fragment)
		}
	}
}

// TestNonChipsCrewLackUniversal verifies the universal addendum is gated to
// crew with declared skills. Maren / Crest / Bosun / Lookout have no skills
// in config/crew.yaml, so their system prompts must not contain the
// Compose-emitted "## Operator Universal Rules" section header. The
// universal addendum is the gate the operator-intent + allowlist + refused-
// outright rules sit under; surfacing it on read-only crew would over-
// constrain personas that have no mutation surface to gate.
func TestNonChipsCrewLackUniversal(t *testing.T) {
	const configPath = "../../config/crew.yaml"
	if _, err := os.Stat(configPath); err != nil {
		t.Skipf("config/crew.yaml not found at %s (running outside repo?)", configPath)
	}
	r, err := crew.Load(configPath)
	if err != nil {
		t.Fatalf("Load real crew.yaml: %v", err)
	}
	const universalHeader = "## Operator Universal Rules"
	for _, id := range []string{"maren", "crest", "bosun", "lookout"} {
		c := r.Get(id)
		if c == nil {
			t.Errorf("%s not found in real crew.yaml", id)
			continue
		}
		if strings.Contains(c.SystemPrompt(), universalHeader) {
			t.Errorf("%s system prompt unexpectedly contains universal-addendum header — gating broken", id)
		}
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

const yamlWithSkills = `
default_crew: chips
crew:
  chips:
    name: "Chips"
    role: "The Carpenter"
    model: "claude-sonnet-4-6"
    verbosity: log-entry
    skills: [github]
    voice:
      model: TBD
      announces_as: "Chips:"
    system_prompt: "You are Chips. Respond in {verbosity}"
`

func TestSkillsParsedFromYAML(t *testing.T) {
	path := writeRegistry(t, yamlWithSkills)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	chips := r.Get("chips")
	if got := chips.Skills(); len(got) != 1 || got[0] != "github" {
		t.Errorf("expected [github], got %v", got)
	}
}

func TestSkillsEmptyWhenOmitted(t *testing.T) {
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := r.Get("maren").Skills(); got != nil {
		t.Errorf("expected nil skills for maren, got %v", got)
	}
}

func TestSkillsRejectsDuplicates(t *testing.T) {
	const dupYAML = `
default_crew: chips
crew:
  chips:
    name: "Chips"
    role: "The Carpenter"
    model: "claude-sonnet-4-6"
    verbosity: log-entry
    skills: [github, github]
    voice:
      model: TBD
      announces_as: "Chips:"
    system_prompt: "You are Chips. Respond in {verbosity}"
`
	path := writeRegistry(t, dupYAML)
	_, err := crew.Load(path)
	if err == nil {
		t.Fatal("expected duplicate-skill error")
	}
	if !strings.Contains(err.Error(), "duplicate skill") {
		t.Errorf("expected duplicate-skill in error, got: %v", err)
	}
}

func TestSkillsRejectsEmptyEntry(t *testing.T) {
	const emptyYAML = `
default_crew: chips
crew:
  chips:
    name: "Chips"
    role: "The Carpenter"
    model: "claude-sonnet-4-6"
    verbosity: log-entry
    skills: [""]
    voice:
      model: TBD
      announces_as: "Chips:"
    system_prompt: "You are Chips. Respond in {verbosity}"
`
	path := writeRegistry(t, emptyYAML)
	_, err := crew.Load(path)
	if err == nil {
		t.Fatal("expected empty-skill-name error")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("expected must-not-be-empty in error, got: %v", err)
	}
}

// ----------------------------------------------------------------------
// Source/Compose wiring (#153)
//
// LoadWithSource invokes skills.Compose for any crew with a non-empty
// `skills:` list and stores the rendered output as the crew's system
// prompt. Crew without skills get persona+verbosity only.
// ----------------------------------------------------------------------

const yamlWithChipsSkill = `
default_crew: chips
crew:
  chips:
    name: "Chips"
    role: "The Carpenter"
    model: "claude-sonnet-4-6"
    verbosity: log-entry
    skills: [github]
    voice:
      model: TBD
      announces_as: "Chips:"
    system_prompt: "You are Chips. Respond in {verbosity}"
`

func TestSystemPromptContainsUniversalSentinels(t *testing.T) {
	path := writeRegistry(t, yamlWithChipsSkill)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out := r.Get("chips").SystemPrompt()
	for _, sentinel := range []string{
		"Operator Universal Rules",    // boundary marker
		"Github Workflow Rules",       // boundary marker (titlecase)
		"Github Profile Addendum",     // boundary marker (titlecase)
		"Operator Intent Required",    // universal addendum content
		"Conventional commits format", // SKILL.md content
	} {
		if !strings.Contains(out, sentinel) {
			t.Errorf("SystemPrompt missing sentinel %q\nfull output:\n%s", sentinel, out)
		}
	}
}

func TestSystemPromptBeginsWithPersona(t *testing.T) {
	path := writeRegistry(t, yamlWithChipsSkill)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out := r.Get("chips").SystemPrompt()
	const wantPrefix = "You are Chips. Respond in Answer in a full paragraph"
	if !strings.HasPrefix(out, wantPrefix) {
		t.Errorf("SystemPrompt must begin with rendered persona, got:\n%s", out)
	}
}

func TestNonSkillsCrewSystemPromptHasNoComposeMarkers(t *testing.T) {
	// validYAML (top of file) has no skills declared on any crew.
	// SystemPrompt should be persona+verbosity only — no Compose-emitted
	// section headings from universal/skill addenda.
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	composeMarkers := []string{
		"## Operator Universal Rules",
		"## Github Workflow Rules",
		"## Github Profile Addendum",
	}
	for _, id := range []string{"maren", "crest", "bosun", "lookout", "chips"} {
		prompt := r.Get(id).SystemPrompt()
		for _, marker := range composeMarkers {
			if strings.Contains(prompt, marker) {
				t.Errorf("crew %s: SystemPrompt unexpectedly contains Compose marker %q (no skills declared)", id, marker)
			}
		}
	}
}

// ----------------------------------------------------------------------
// LoadWithSource — test injection via mapSource
// ----------------------------------------------------------------------

// mapSource is a test-only Source backed by an in-memory map keyed by
// canonical relative path (slash-separated, matching Doc.Path contract).
// Lets LoadWithSource tests inject precise content without depending on
// the embedded fixtures.
type mapSource map[string]string

func (m mapSource) Universal() (skills.Doc, error) {
	if c, ok := m["_universal.md"]; ok {
		return skills.Doc{Path: "_universal.md", Content: c}, nil
	}
	return skills.Doc{}, skills.ErrNotFound
}

func (m mapSource) Skill(name string) (skills.Doc, error) {
	key := name + "/SKILL.md"
	if c, ok := m[key]; ok {
		return skills.Doc{Path: key, Content: c}, nil
	}
	return skills.Doc{}, skills.ErrNotFound
}

func (m mapSource) Profile(name string) (skills.Doc, error) {
	key := name + "/profile.md"
	if c, ok := m[key]; ok {
		return skills.Doc{Path: key, Content: c}, nil
	}
	return skills.Doc{}, skills.ErrNotFound
}

func TestLoadWithSourceFailsOnUnknownSkill(t *testing.T) {
	// Empty source → Compose wraps ErrNotFound for the missing skill →
	// LoadWithSource propagates with the crew id in the error.
	path := writeRegistry(t, yamlWithChipsSkill)
	_, err := crew.LoadWithSource(path, mapSource{})
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
	if !errors.Is(err, skills.ErrNotFound) {
		t.Errorf("expected wrapped ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "chips") {
		t.Errorf("expected error to mention crew id chips, got %v", err)
	}
}

func TestLoadWithSourceUsesInjectedSource(t *testing.T) {
	// Hermetic: injected source provides distinctive sentinels that
	// EmbeddedSource doesn't. SystemPrompt must contain them, proving
	// LoadWithSource routed through the injected source.
	path := writeRegistry(t, yamlWithChipsSkill)
	src := mapSource{
		"_universal.md":     "INJECTED-UNIV-SENTINEL",
		"github/SKILL.md":   "INJECTED-SKILL-SENTINEL",
		"github/profile.md": "INJECTED-PROFILE-SENTINEL",
	}
	r, err := crew.LoadWithSource(path, src)
	if err != nil {
		t.Fatalf("LoadWithSource: %v", err)
	}
	out := r.Get("chips").SystemPrompt()
	for _, want := range []string{
		"INJECTED-UNIV-SENTINEL",
		"INJECTED-SKILL-SENTINEL",
		"INJECTED-PROFILE-SENTINEL",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SystemPrompt missing injected sentinel %q", want)
		}
	}
}

// ----------------------------------------------------------------------
// ValidateSkills (mirrors ValidateTools)
// ----------------------------------------------------------------------

func TestValidateSkillsAllPresent(t *testing.T) {
	path := writeRegistry(t, yamlWithChipsSkill)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := r.ValidateSkills(skills.EmbeddedSource{}); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateSkillsMissing(t *testing.T) {
	// Build the registry via LoadWithSource with a source that DOES
	// have github (so Load succeeds), then ValidateSkills against an
	// empty source — proving the validator surfaces missing skills
	// regardless of which source was used at Load time. An empty
	// source also has no universal addendum, so we expect both the
	// per-skill issue AND the universal-missing issue.
	path := writeRegistry(t, yamlWithChipsSkill)
	loadSrc := mapSource{
		"_universal.md":     "UNIV",
		"github/SKILL.md":   "SKILL",
		"github/profile.md": "PROFILE",
	}
	r, err := crew.LoadWithSource(path, loadSrc)
	if err != nil {
		t.Fatalf("LoadWithSource: %v", err)
	}
	err = r.ValidateSkills(mapSource{})
	if err == nil {
		t.Fatal("expected validation error for missing skill")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Errorf("expected error to mention skill name github, got: %v", err)
	}
	if !strings.Contains(err.Error(), "chips") {
		t.Errorf("expected error to mention crew id chips, got: %v", err)
	}
	if !strings.Contains(err.Error(), "universal addendum missing") {
		t.Errorf("expected error to mention missing universal addendum, got: %v", err)
	}
}

func TestValidateSkillsRequiresUniversalWhenSkillsDeclared(t *testing.T) {
	// Source has the github SKILL.md but no _universal.md. With at
	// least one crew declaring skills, ValidateSkills must surface a
	// universal-missing issue even though every per-skill name resolves.
	// (Compose's ErrUniversalRequired is the runtime equivalent of
	// this; ValidateSkills lets operators catch it before runtime.)
	path := writeRegistry(t, yamlWithChipsSkill)
	loadSrc := mapSource{
		"_universal.md":     "UNIV",
		"github/SKILL.md":   "SKILL",
		"github/profile.md": "PROFILE",
	}
	r, err := crew.LoadWithSource(path, loadSrc)
	if err != nil {
		t.Fatalf("LoadWithSource: %v", err)
	}
	candidate := mapSource{
		// no _universal.md — the candidate would silently pass a
		// skills-only validator, which is the bug we're guarding.
		"github/SKILL.md":   "SKILL",
		"github/profile.md": "PROFILE",
	}
	err = r.ValidateSkills(candidate)
	if err == nil {
		t.Fatal("expected validation error for missing universal")
	}
	if !strings.Contains(err.Error(), "universal addendum missing") {
		t.Errorf("expected universal-missing message, got: %v", err)
	}
	// The per-skill check must NOT fire (github resolves) — pin that
	// the per-skill and universal paths are independent.
	if strings.Contains(err.Error(), "unknown skill") {
		t.Errorf("github resolves; should not report unknown skill: %v", err)
	}
}

func TestValidateSkillsSkipsUniversalCheckWhenNoSkillsDeclared(t *testing.T) {
	// validYAML has no skills declared on any crew, so universal is
	// not a prerequisite. A checker with no Universal must NOT trigger
	// a universal-missing issue.
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := r.ValidateSkills(mapSource{}); err != nil {
		t.Errorf("expected no error when no skills declared, got: %v", err)
	}
}

func TestValidateSkillsUniversalErrorClasses(t *testing.T) {
	// Mirror the per-skill class test for the universal path. Skill
	// returns nil so per-skill issues don't pollute; only the
	// universal branch triggers.
	path := writeRegistry(t, yamlWithChipsSkill)
	loadSrc := mapSource{
		"_universal.md":     "UNIV",
		"github/SKILL.md":   "SKILL",
		"github/profile.md": "PROFILE",
	}
	r, err := crew.LoadWithSource(path, loadSrc)
	if err != nil {
		t.Fatalf("LoadWithSource: %v", err)
	}

	cases := []struct {
		name         string
		universalErr error
		wantSubstr   string
	}{
		{
			name:         "ErrNotFound → universal addendum missing",
			universalErr: skills.ErrNotFound,
			wantSubstr:   "universal addendum missing",
		},
		{
			name:         "other error → validating universal addendum: <err>",
			universalErr: errors.New("permission denied"),
			wantSubstr:   "validating universal addendum",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// skillErr=nil so per-skill branch never fires; only the
			// universal branch can surface an issue.
			src := errorSource{universalErr: tc.universalErr, skillErr: nil}
			// With skillErr=nil, errorSource.Skill returns (zero Doc, nil) —
			// ValidateSkills treats that as "skill resolved".
			err := r.ValidateSkills(src)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("expected error to contain %q, got: %v", tc.wantSubstr, err)
			}
		})
	}
}

// errorSource is a test-only Source with configurable per-method
// errors. Used by ValidateSkills tests to exercise the
// per-error-class branches (ErrNotFound, ErrInvalidSkillName, other)
// for both Universal and Skill independently. Universal succeeds by
// default so per-skill tests aren't polluted with a spurious
// universal-missing issue; tests that want to exercise the universal
// check set universalErr explicitly.
type errorSource struct {
	skillErr     error
	universalErr error
}

func (e errorSource) Universal() (skills.Doc, error) {
	if e.universalErr != nil {
		return skills.Doc{}, e.universalErr
	}
	return skills.Doc{Path: "_universal.md", Content: "UNIV"}, nil
}

func (e errorSource) Skill(_ string) (skills.Doc, error) {
	return skills.Doc{}, e.skillErr
}

func (e errorSource) Profile(_ string) (skills.Doc, error) {
	return skills.Doc{}, skills.ErrNotFound
}

func TestValidateSkillsDistinguishesErrorClasses(t *testing.T) {
	// Build registry with chips:[github] declared; vary the checker's
	// returned error to exercise each branch of the switch.
	path := writeRegistry(t, yamlWithChipsSkill)
	loadSrc := mapSource{
		"_universal.md":     "UNIV",
		"github/SKILL.md":   "SKILL",
		"github/profile.md": "PROFILE",
	}
	r, err := crew.LoadWithSource(path, loadSrc)
	if err != nil {
		t.Fatalf("LoadWithSource: %v", err)
	}

	cases := []struct {
		name        string
		checkerErr  error
		wantSubstr  string
		wantNoSubstr string // optional: must NOT appear
	}{
		{
			name:       "ErrNotFound → unknown skill",
			checkerErr: skills.ErrNotFound,
			wantSubstr: "unknown skill",
		},
		{
			name:       "ErrInvalidSkillName → invalid skill name",
			checkerErr: skills.ErrInvalidSkillName,
			wantSubstr: "invalid skill name",
			// must NOT misreport as "unknown" — that's the bug fix.
			wantNoSubstr: "unknown skill",
		},
		{
			name:       "other error → validating skill: <err>",
			checkerErr: errors.New("permission denied"),
			wantSubstr: "validating skill",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.ValidateSkills(errorSource{skillErr: tc.checkerErr})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("expected error to contain %q, got: %v", tc.wantSubstr, err)
			}
			if tc.wantNoSubstr != "" && strings.Contains(err.Error(), tc.wantNoSubstr) {
				t.Errorf("error should NOT contain %q (misreport bug); got: %v", tc.wantNoSubstr, err)
			}
		})
	}
}

func TestValidateSkillsNoSkillsDeclared(t *testing.T) {
	// Empty checker — no skills resolvable, but no skills declared
	// either. Mirrors TestValidateToolsNoToolsDeclared.
	path := writeRegistry(t, validYAML)
	r, err := crew.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := r.ValidateSkills(mapSource{}); err != nil {
		t.Errorf("expected no error when no skills declared, got: %v", err)
	}
}

func TestEmbeddedSourceContainsAllSkillsDeclaredInRealCrewYAML(t *testing.T) {
	// Pin the contract that every skill declared in the real
	// config/crew.yaml resolves via the EmbeddedSource — equivalent
	// in spirit to validating production tool registry against the
	// production tool list.
	configPath := "../../config/crew.yaml"
	if _, err := os.Stat(configPath); err != nil {
		t.Skipf("real config/crew.yaml not present (running outside repo?): %v", err)
	}
	r, err := crew.Load(configPath)
	if err != nil {
		t.Fatalf("Load real crew.yaml: %v", err)
	}
	if err := r.ValidateSkills(skills.EmbeddedSource{}); err != nil {
		t.Errorf("ValidateSkills against EmbeddedSource: %v", err)
	}
}

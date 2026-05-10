package crew

import (
	_ "embed"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"klazomenai/bridge/internal/crew/skills"
)

// chipsGitHubSkill is the curated github skill body, vendored from the operator's
// dotfiles. Appended to Chips' system prompt at registry-load time so the persona
// inherits the operator's standing rules on commits, branches, PRs, and review
// threads. Re-sync via the command in CONTRIBUTING.md.
//
//go:embed skills/github.md
var chipsGitHubSkill string

// verbosityDescriptions maps verbosity mode names to their injected instruction text.
var verbosityDescriptions = map[string]string{
	"signal":            "Answer in exactly 1 sentence covering the key fact only.",
	"dispatch":          "Answer in 3-5 sentences covering the key point and essential context only.",
	"log-entry":         "Answer in a full paragraph with complete reasoning.",
	"ships-orders":      "Answer in a numbered or bulleted list suitable for reading aloud.",
	"captains-briefing": "Answer comprehensively with full reasoning and structured points, for complex decisions.",
}

// crewYAML mirrors the shape of config/crew.yaml.
type crewYAML struct {
	DefaultCrew string                 `yaml:"default_crew"`
	Crew        map[string]crewEntryYAML `yaml:"crew"`
}

type crewEntryYAML struct {
	Name         string    `yaml:"name"`
	Role         string    `yaml:"role"`
	Model        string    `yaml:"model"`
	Verbosity    string    `yaml:"verbosity"`
	Voice        voiceYAML `yaml:"voice"`
	SystemPrompt string    `yaml:"system_prompt"`
	Tools        []string  `yaml:"tools"`
	// Skills is the optional list of skill names whose SKILL.md (and
	// optional profile.md) get appended to the crew member's system
	// prompt at registry-load time via the skills package's Compose
	// function. Empty / omitted means the crew gets persona+verbosity
	// only — current Maren/Crest/Bosun/Lookout behaviour.
	Skills []string `yaml:"skills"`
}

type voiceYAML struct {
	Model       string `yaml:"model"`
	AnnouncesAs string `yaml:"announces_as"`
}

// Registry holds the loaded crew members.
type Registry struct {
	defaultCrew string
	crew        map[string]Crew
}

// Load parses the crew YAML at path and returns a Registry. Skills
// declared on crew entries (via the `skills: []` field) are composed
// against an EmbeddedSource by default — production callers wanting
// to layer a filesystem override on top should call LoadWithSource
// with a FallbackSource.
func Load(path string) (*Registry, error) {
	return LoadWithSource(path, skills.EmbeddedSource{})
}

// LoadWithSource is Load with a caller-provided skills.Source. Used by
// tests (injecting a MapSource for hermetic Compose-output assertions)
// and by future callers (e.g. main.go) that want to layer a filesystem
// override over the embedded fallback.
//
// For each crew entry with a non-empty `skills:` list, this function
// invokes skills.Compose against the supplied source and stores the
// rendered prompt on BaseCrew.composeOutput, reachable via
// BaseCrew.ComposeOutput. The legacy `if id == "chips"` gate remains
// the source of truth for SystemPrompt() in this sub-PR — the gate
// flip happens in #154 (PR4). This dual-path arrangement gives PR4's
// L2 enforcement tests an A/B affordance: legacy output through
// SystemPrompt(), new output through ComposeOutput(), both renderable
// without changing crew.yaml.
//
// A declared skill that fails to resolve (Compose returns an error)
// causes LoadWithSource to fail. Callers must supply a source that
// covers every skill name declared in crew.yaml, or use ValidateSkills
// up front to list missing skills before constructing the registry.
func LoadWithSource(path string, source skills.Source) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading crew registry %s: %w", path, err)
	}

	var cfg crewYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing crew registry: %w", err)
	}

	if cfg.DefaultCrew == "" {
		return nil, fmt.Errorf("crew registry: default_crew is required")
	}
	if cfg.DefaultCrew != strings.ToLower(cfg.DefaultCrew) {
		return nil, fmt.Errorf("crew registry: default_crew %q must be lowercase", cfg.DefaultCrew)
	}

	registry := &Registry{
		defaultCrew: cfg.DefaultCrew,
		crew:        make(map[string]Crew, len(cfg.Crew)),
	}

	for id, entry := range cfg.Crew {
		if id != strings.ToLower(id) {
			return nil, fmt.Errorf("crew registry: crew ID %q must be lowercase", id)
		}
		if err := validateEntry(id, entry); err != nil {
			return nil, err
		}
		verbDesc, ok := verbosityDescriptions[entry.Verbosity]
		if !ok {
			return nil, fmt.Errorf("crew %s: unknown verbosity %q", id, entry.Verbosity)
		}
		prompt := strings.ReplaceAll(entry.SystemPrompt, "{verbosity}", verbDesc)
		if id == "chips" {
			// TrimRight bounds the separator on the prompt side: YAML `|`
			// literal block scalars produce a trailing newline today, but
			// `|-` would strip it. Normalising to exactly one blank line
			// keeps the boundary stable regardless of YAML chomp style.
			prompt = strings.TrimRight(prompt, "\n") + "\n\n" + chipsGitHubSkill
		}

		// Dual-path: render the Compose-based prompt for any crew with
		// declared skills. The legacy id-gate above still wins for
		// SystemPrompt() in this PR; the new prompt is reachable via
		// ComposeOutput() so PR4's L2 tests can A/B both paths before
		// the gate flip.
		var composeOutput string
		if len(entry.Skills) > 0 {
			persona := strings.ReplaceAll(entry.SystemPrompt, "{verbosity}", verbDesc)
			composed, err := skills.Compose(persona, entry.Skills, source)
			if err != nil {
				return nil, fmt.Errorf("crew %s: %w", id, err)
			}
			composeOutput = composed
		}

		registry.crew[id] = &BaseCrew{
			id:            id,
			name:          entry.Name,
			role:          entry.Role,
			model:         entry.Model,
			verbosity:     entry.Verbosity,
			systemPrompt:  prompt,
			composeOutput: composeOutput,
			announcesAs:   entry.Voice.AnnouncesAs,
			voiceModel:    entry.Voice.Model,
			tools:         entry.Tools,
			skills:        entry.Skills,
		}
	}

	if _, ok := registry.crew[cfg.DefaultCrew]; !ok {
		return nil, fmt.Errorf("crew registry: default_crew %q not found in crew list", cfg.DefaultCrew)
	}

	return registry, nil
}

func validateEntry(id string, e crewEntryYAML) error {
	if e.Name == "" {
		return fmt.Errorf("crew %s: name is required", id)
	}
	if e.Role == "" {
		return fmt.Errorf("crew %s: role is required", id)
	}
	if e.Model == "" {
		return fmt.Errorf("crew %s: model is required", id)
	}
	if e.Verbosity == "" {
		return fmt.Errorf("crew %s: verbosity is required", id)
	}
	if e.SystemPrompt == "" {
		return fmt.Errorf("crew %s: system_prompt is required", id)
	}
	if e.Voice.Model == "" {
		return fmt.Errorf("crew %s: voice.model is required", id)
	}
	if e.Voice.AnnouncesAs == "" {
		return fmt.Errorf("crew %s: voice.announces_as is required", id)
	}
	seen := make(map[string]bool, len(e.Skills))
	for _, name := range e.Skills {
		if name == "" {
			return fmt.Errorf("crew %s: skill name must not be empty", id)
		}
		if seen[name] {
			return fmt.Errorf("crew %s: duplicate skill %q", id, name)
		}
		seen[name] = true
	}
	return nil
}

// Get returns the crew member by ID, or nil if not found.
func (r *Registry) Get(id string) Crew {
	return r.crew[id]
}

// Default returns the default crew member.
func (r *Registry) Default() Crew {
	return r.crew[r.defaultCrew]
}

// DefaultID returns the default crew ID.
func (r *Registry) DefaultID() string {
	return r.defaultCrew
}

// IDs returns all crew IDs in the registry.
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.crew))
	for id := range r.crew {
		ids = append(ids, id)
	}
	return ids
}

// ToolChecker is the interface used by ValidateTools to check tool existence
// without importing the tools package (avoids circular dependency).
type ToolChecker interface {
	Has(name string) bool
}

// ValidateTools checks that every tool declared in crew.yaml exists in the
// tool registry. Returns an error listing all missing tools.
func (r *Registry) ValidateTools(checker ToolChecker) error {
	var missing []string
	for id, c := range r.crew {
		for _, tool := range c.Tools() {
			if !checker.Has(tool) {
				missing = append(missing, fmt.Sprintf("crew %s: unknown tool %q", id, tool))
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("tool validation failed:\n  %s", strings.Join(missing, "\n  "))
}

// SkillChecker is the narrow interface used by ValidateSkills. The
// skills.Source interface satisfies it (Skill is one of its three
// methods), so production callers pass the same source they used at
// LoadWithSource time. A custom checker can also be supplied for
// pre-load consistency checks (e.g. comparing a candidate dotfiles
// ref against the currently-declared crew.yaml).
type SkillChecker interface {
	Skill(name string) (skills.Doc, error)
}

// ValidateSkills checks that every skill declared in crew.yaml resolves
// against checker. Returns a sorted, comma-separated error listing all
// missing skills, or nil when every entry resolves cleanly. Mirrors
// ValidateTools.
//
// LoadWithSource already fails fast on unresolvable skills via Compose's
// wrapped ErrNotFound. ValidateSkills exists for pre-load checks and
// for parity with ValidateTools — main.go can call both after Load to
// surface inconsistencies independently of the load path.
func (r *Registry) ValidateSkills(checker SkillChecker) error {
	var missing []string
	for id, c := range r.crew {
		for _, name := range c.Skills() {
			if _, err := checker.Skill(name); err != nil {
				missing = append(missing, fmt.Sprintf("crew %s: unknown skill %q (not in skill source)", id, name))
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("skill validation failed:\n  %s", strings.Join(missing, "\n  "))
}

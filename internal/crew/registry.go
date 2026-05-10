package crew

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"klazomenai/bridge/internal/crew/skills"
)

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
	// optional profile.md) drive the Compose-rendered system prompt for
	// this crew member at registry-load time. Empty / omitted means the
	// crew gets persona+verbosity only.
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
// tests (injecting a mapSource for hermetic Compose-output assertions)
// and by callers (e.g. main.go) that want to layer a filesystem override
// over the embedded fallback.
//
// For each crew entry with a non-empty `skills:` list, this function
// invokes skills.Compose against the supplied source and stores the
// rendered output as the crew's system prompt. Crew with no declared
// skills get persona+verbosity only.
//
// LoadWithSource fails fast on any skill that does not resolve via the
// supplied source: Compose's wrapped ErrNotFound (or ErrUniversalRequired
// for a missing universal) propagates out. To pre-flight a candidate
// source against an already-loaded registry — e.g. comparing an
// updated dotfiles ref against the currently-deployed crew.yaml before
// rolling out — first load with a known-good source (typically the
// default EmbeddedSource via Load), then call Registry.ValidateSkills
// against the candidate.
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
		persona := strings.ReplaceAll(entry.SystemPrompt, "{verbosity}", verbDesc)
		systemPrompt := persona
		var composeOutput string
		if len(entry.Skills) > 0 {
			composed, err := skills.Compose(persona, entry.Skills, source)
			if err != nil {
				return nil, fmt.Errorf("crew %s: %w", id, err)
			}
			systemPrompt = composed
			composeOutput = composed
		}

		registry.crew[id] = &BaseCrew{
			id:            id,
			name:          entry.Name,
			role:          entry.Role,
			model:         entry.Model,
			verbosity:     entry.Verbosity,
			systemPrompt:  systemPrompt,
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

// SkillChecker is the narrow interface used by ValidateSkills. It
// covers exactly what ValidateSkills needs to verify Compose's
// prerequisites at this caller layer: the universal addendum (required
// when any crew has declared skills) and each declared skill's
// SKILL.md. Profile is deliberately omitted since Compose treats
// missing profiles as soft.
//
// skills.Source satisfies SkillChecker via duck typing, so production
// callers pass the same source they used at LoadWithSource time. A
// custom checker can also be supplied to compare a candidate source
// against the loaded registry without re-running Load.
type SkillChecker interface {
	Universal() (skills.Doc, error)
	Skill(name string) (skills.Doc, error)
}

// ValidateSkills checks that the supplied checker resolves the full set
// of inputs Compose would need at registry load time:
//
//  1. The universal addendum (_universal.md), required when any crew
//     has at least one declared skill — Compose's ErrUniversalRequired
//     fires otherwise.
//  2. Each declared skill's SKILL.md, per crew.
//
// Returns nil when every prerequisite resolves cleanly, or an
// aggregated error formatted as a newline-indented bullet list of
// per-crew issues (mirroring ValidateTools). Issue messages distinguish
// between three failure modes so operators can diagnose without
// chasing the source error:
//
//   - skills.ErrNotFound         → "unknown skill" (or "universal addendum missing")
//   - skills.ErrInvalidSkillName → "invalid skill name" (regex mismatch)
//   - any other error            → "validating skill: <err>" (I/O, etc.)
//
// LoadWithSource already fails fast on unresolvable skills via Compose's
// wrapped ErrNotFound (or ErrUniversalRequired). ValidateSkills exists
// primarily for pre-deployment consistency checks against a *candidate*
// source — load with a known-good source first, then call ValidateSkills
// against the candidate to list every issue without committing to a
// reload.
func (r *Registry) ValidateSkills(checker SkillChecker) error {
	var issues []string
	anySkills := false

	// Per-crew pass: compute anySkills inline and walk each declared
	// skill exactly once. Caching c.Skills() per crew avoids the
	// double defensive-copy that Skills() incurs.
	for id, c := range r.crew {
		skillNames := c.Skills()
		if len(skillNames) > 0 {
			anySkills = true
		}
		for _, name := range skillNames {
			_, err := checker.Skill(name)
			if err == nil {
				continue
			}
			switch {
			case errors.Is(err, skills.ErrNotFound):
				issues = append(issues, fmt.Sprintf("crew %s: unknown skill %q (not in skill source)", id, name))
			case errors.Is(err, skills.ErrInvalidSkillName):
				issues = append(issues, fmt.Sprintf("crew %s: invalid skill name %q (must match %s)", id, name, skills.SkillNameConstraint))
			default:
				issues = append(issues, fmt.Sprintf("crew %s: validating skill %q: %v", id, name, err))
			}
		}
	}

	// Universal is a prerequisite only when at least one crew has
	// declared skills (matches Compose's behaviour: empty skills slice
	// returns the persona unchanged without consulting Universal).
	// Run after the per-skill pass — sort.Strings below normalises
	// output order regardless of insertion order.
	if anySkills {
		if _, err := checker.Universal(); err != nil {
			switch {
			case errors.Is(err, skills.ErrNotFound):
				issues = append(issues, `universal addendum missing ("_universal.md" not in skill source)`)
			default:
				issues = append(issues, fmt.Sprintf("validating universal addendum: %v", err))
			}
		}
	}

	if len(issues) == 0 {
		return nil
	}
	sort.Strings(issues)
	return fmt.Errorf("skill validation failed:\n  %s", strings.Join(issues, "\n  "))
}

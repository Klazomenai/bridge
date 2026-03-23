package crew

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
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

// Load parses the crew YAML at path and returns a Registry.
func Load(path string) (*Registry, error) {
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
		registry.crew[id] = &BaseCrew{
			id:           id,
			name:         entry.Name,
			role:         entry.Role,
			model:        entry.Model,
			verbosity:    entry.Verbosity,
			systemPrompt: prompt,
			announcesAs:  entry.Voice.AnnouncesAs,
			voiceModel:   entry.Voice.Model,
			tools:        entry.Tools,
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

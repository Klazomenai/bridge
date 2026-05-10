package crew

// Crew defines the interface that all crew members implement.
type Crew interface {
	ID() string
	Name() string
	Role() string
	Model() string
	Verbosity() string
	SystemPrompt() string
	AnnouncesAs() string
	VoiceModel() string
	Tools() []string
	// Skills returns the names of skills declared in crew.yaml's
	// `skills:` field. Each name resolves to a `<name>/SKILL.md` (and
	// optional `<name>/profile.md`) in the skills source. Returns nil
	// for crew with no skills declared.
	Skills() []string
	// ComposeOutput returns the skills.Compose-rendered prompt for crew
	// with declared skills. Empty for crew without skills. Until the
	// id-gate flip in #154, this is informational — SystemPrompt() is
	// the runtime source of truth. After the flip, ComposeOutput will
	// either be the source of truth or be removed entirely.
	ComposeOutput() string
}

// BaseCrew holds the parsed crew member configuration.
//
// composeOutput holds the skills.Compose-rendered prompt for crew with
// declared skills, populated by LoadWithSource alongside the
// id-gate-driven systemPrompt. Until the gate flip in #154 the field
// is informational; ComposeOutput exposes it so PR4's L2 tests can
// A/B both paths.
type BaseCrew struct {
	id            string
	name          string
	role          string
	model         string
	verbosity     string
	systemPrompt  string
	composeOutput string
	announcesAs   string
	voiceModel    string
	tools         []string
	skills        []string
}

func (c *BaseCrew) ID() string            { return c.id }
func (c *BaseCrew) Name() string          { return c.name }
func (c *BaseCrew) Role() string          { return c.role }
func (c *BaseCrew) Model() string         { return c.model }
func (c *BaseCrew) Verbosity() string     { return c.verbosity }
func (c *BaseCrew) SystemPrompt() string  { return c.systemPrompt }
func (c *BaseCrew) ComposeOutput() string { return c.composeOutput }
func (c *BaseCrew) AnnouncesAs() string   { return c.announcesAs }
func (c *BaseCrew) VoiceModel() string    { return c.voiceModel }
func (c *BaseCrew) Tools() []string {
	if len(c.tools) == 0 {
		return nil
	}
	out := make([]string, len(c.tools))
	copy(out, c.tools)
	return out
}

// Skills returns a copy of the crew's declared skill names. Returns nil
// when no skills are declared.
func (c *BaseCrew) Skills() []string {
	if len(c.skills) == 0 {
		return nil
	}
	out := make([]string, len(c.skills))
	copy(out, c.skills)
	return out
}

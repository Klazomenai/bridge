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
}

// BaseCrew holds the parsed crew member configuration.
type BaseCrew struct {
	id           string
	name         string
	role         string
	model        string
	verbosity    string
	systemPrompt string
	announcesAs  string
	voiceModel   string
	tools        []string
}

func (c *BaseCrew) ID() string           { return c.id }
func (c *BaseCrew) Name() string         { return c.name }
func (c *BaseCrew) Role() string         { return c.role }
func (c *BaseCrew) Model() string        { return c.model }
func (c *BaseCrew) Verbosity() string    { return c.verbosity }
func (c *BaseCrew) SystemPrompt() string { return c.systemPrompt }
func (c *BaseCrew) AnnouncesAs() string  { return c.announcesAs }
func (c *BaseCrew) VoiceModel() string   { return c.voiceModel }
func (c *BaseCrew) Tools() []string {
	if len(c.tools) == 0 {
		return nil
	}
	out := make([]string, len(c.tools))
	copy(out, c.tools)
	return out
}

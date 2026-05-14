// Package orchestrator routes Matrix messages to the appropriate crew member
// and calls the Anthropic API with the full session context.
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	ctxbuf "klazomenai/bridge/internal/context"
	"klazomenai/bridge/internal/crew"
	"klazomenai/bridge/internal/tools"
	"klazomenai/bridge/internal/tools/redact"
)

const (
	// maxUserMessageLen caps input to limit prompt injection surface.
	maxUserMessageLen = 1000
	// captainPrefix is prepended to all user messages.
	captainPrefix = "The Captain says: "
	// maxTokens is the response token budget.
	maxTokens = 1024
	// maxDelegationDepth caps crew-to-crew delegation chains to prevent loops.
	maxDelegationDepth = 2
)

// Response is what the orchestrator returns after calling Claude.
type Response struct {
	Text       string
	CrewID     string
	CrewMember string
	Verbosity  string
}

// ClaudeClient is the interface satisfied by *anthropic.MessageService.
// Using an interface allows test doubles without a real API key.
type ClaudeClient interface {
	New(ctx context.Context, body anthropic.MessageNewParams, opts ...option.RequestOption) (*anthropic.Message, error)
}

// Orchestrator routes messages to crew members and calls the Anthropic API.
type Orchestrator struct {
	registry   *crew.Registry
	context    *ctxbuf.Manager
	client     ClaudeClient
	tools      *tools.Registry
	sandboxCfg tools.SandboxConfig
}

// New creates an Orchestrator with the real Anthropic client.
// apiKey is the Anthropic API key read from the mounted secret file.
func New(registry *crew.Registry, ctxManager *ctxbuf.Manager, toolReg *tools.Registry, apiKey string) *Orchestrator {
	if toolReg == nil {
		panic("orchestrator: toolReg must not be nil")
	}
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &Orchestrator{
		registry:   registry,
		context:    ctxManager,
		client:     &c.Messages,
		tools:      toolReg,
		sandboxCfg: tools.DefaultSandboxConfig(),
	}
}

// NewWithClient creates an Orchestrator with a custom ClaudeClient (for testing).
func NewWithClient(registry *crew.Registry, ctxManager *ctxbuf.Manager, toolReg *tools.Registry, client ClaudeClient) *Orchestrator {
	if toolReg == nil {
		panic("orchestrator: toolReg must not be nil")
	}
	return &Orchestrator{
		registry:   registry,
		context:    ctxManager,
		tools:      toolReg,
		client:     client,
		sandboxCfg: tools.DefaultSandboxConfig(),
	}
}

// SetSandboxConfig overrides the default sandbox configuration.
// Must be called before any Handle() calls. Intended for testing.
func (o *Orchestrator) SetSandboxConfig(cfg tools.SandboxConfig) {
	o.sandboxCfg = cfg
}

// Route selects the crew member for this message.
// If requestedCrew is non-empty and exists in the registry, it is used.
// Otherwise the default crew member is returned.
func (o *Orchestrator) Route(requestedCrew string) crew.Crew {
	if requestedCrew != "" {
		if c := o.registry.Get(requestedCrew); c != nil {
			return c
		}
		slog.Warn("orchestrator: unknown crew member, falling back to default",
			"requested", redact.Sanitise(requestedCrew), "default", o.registry.DefaultID())
	}
	return o.registry.Default()
}

// Handle processes a message from roomID, routes it to the appropriate crew member,
// calls the Anthropic API with session context, and returns the responses.
// If a crew member delegates to another, multiple responses are returned.
// requestedCrew may be empty (use default) or a crew ID.
func (o *Orchestrator) Handle(ctx context.Context, roomID, userText, requestedCrew string) ([]Response, error) {
	return o.handleWithDepth(ctx, roomID, userText, requestedCrew, 0)
}

func (o *Orchestrator) handleWithDepth(ctx context.Context, roomID, userText, requestedCrew string, depth int) ([]Response, error) {
	c := o.Route(requestedCrew)

	framed := frameMessage(userText)

	buf := o.context.Buffer(roomID)
	history := buf.Messages()

	userMsg := anthropic.NewUserMessage(anthropic.NewTextBlock(framed))
	messages := append(history, userMsg)

	slog.Info("orchestrator: calling claude",
		"crew", c.Name(), "room", roomID,
		"history_turns", len(history)/2, "model", c.Model(),
		"delegation_depth", depth)

	result, err := o.runToolLoop(ctx, c, roomID, buf, userMsg, messages)
	if err != nil {
		return nil, err
	}

	// Store all turns in the context buffer.
	buf.Add(userMsg)
	for _, turn := range result.turns {
		buf.Add(turn)
	}
	if result.text != "" && result.delegation == nil {
		buf.Add(anthropic.NewAssistantMessage(anthropic.NewTextBlock(result.text)))
	}

	var responses []Response
	if result.text != "" {
		responses = append(responses, *buildResponse(c, result.text))
	}

	// If the crew member delegated, recursively handle the delegated crew.
	if result.delegation != nil && depth < maxDelegationDepth {
		safeDelegateID := redact.Sanitise(result.delegation.crewID)
		delegateText := fmt.Sprintf("[%s delegates]: %s", c.Name(), result.delegation.context)
		slog.Info("orchestrator: following delegation",
			"from", c.ID(), "to", safeDelegateID,
			"room", roomID, "depth", depth+1)
		more, err := o.handleWithDepth(ctx, roomID, delegateText, result.delegation.crewID, depth+1)
		if err != nil {
			slog.Warn("orchestrator: delegation failed, returning partial responses",
				"to", safeDelegateID, "err", err)
			return responses, nil
		}
		responses = append(responses, more...)
	} else if result.delegation != nil {
		safeDelegateID := redact.Sanitise(result.delegation.crewID)
		slog.Warn("orchestrator: delegation depth exceeded",
			"from", c.ID(), "to", safeDelegateID,
			"depth", depth, "max", maxDelegationDepth)
		// If the crew delegated with no text, the user would receive nothing.
		// Add an explanation so the delegation limit is visible.
		if len(responses) == 0 {
			responses = append(responses, Response{
				Text:       fmt.Sprintf("[delegation to %s not followed — depth limit reached]", safeDelegateID),
				CrewID:     c.ID(),
				CrewMember: c.Name(),
				Verbosity:  c.Verbosity(),
			})
		}
	}

	return responses, nil
}

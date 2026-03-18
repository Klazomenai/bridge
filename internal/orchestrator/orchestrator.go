// Package orchestrator routes Matrix messages to the appropriate crew member
// and calls the Anthropic API with the full session context.
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	ctxbuf "klazomenai/bridge/internal/context"
	"klazomenai/bridge/internal/crew"
)

const (
	// maxUserMessageLen caps input to limit prompt injection surface.
	maxUserMessageLen = 1000
	// captainPrefix is prepended to all user messages.
	captainPrefix = "The Captain says: "
	// maxTokens is the response token budget.
	maxTokens = 1024
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
	registry *crew.Registry
	context  *ctxbuf.Manager
	client   ClaudeClient
}

// New creates an Orchestrator with the real Anthropic client.
// apiKey is the Anthropic API key read from the mounted secret file.
func New(registry *crew.Registry, ctxManager *ctxbuf.Manager, apiKey string) *Orchestrator {
	c := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &Orchestrator{
		registry: registry,
		context:  ctxManager,
		client:   &c.Messages,
	}
}

// NewWithClient creates an Orchestrator with a custom ClaudeClient (for testing).
func NewWithClient(registry *crew.Registry, ctxManager *ctxbuf.Manager, client ClaudeClient) *Orchestrator {
	return &Orchestrator{registry: registry, context: ctxManager, client: client}
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
			"requested", requestedCrew, "default", o.registry.DefaultID())
	}
	return o.registry.Default()
}

// Handle processes a message from roomID, routes it to the appropriate crew member,
// calls the Anthropic API with session context, and returns the response.
// requestedCrew may be empty (use default) or a crew ID.
func (o *Orchestrator) Handle(ctx context.Context, roomID, userText, requestedCrew string) (*Response, error) {
	c := o.Route(requestedCrew)

	// Cap and frame the user message to limit prompt injection surface.
	if len(userText) > maxUserMessageLen {
		userText = userText[:maxUserMessageLen]
	}
	framed := captainPrefix + userText

	buf := o.context.Buffer(roomID)
	history := buf.Messages()

	userMsg := anthropic.NewUserMessage(anthropic.NewTextBlock(framed))
	messages := append(history, userMsg)

	slog.Info("orchestrator: calling claude",
		"crew", c.Name(), "room", roomID,
		"history_turns", len(history)/2, "model", c.Model())

	resp, err := o.client.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.Model()),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: c.SystemPrompt()},
		},
		Messages: messages,
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic api: %w", err)
	}

	var sb strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	text := strings.TrimSpace(sb.String())

	// Add both turns to the context buffer.
	buf.Add(userMsg)
	buf.Add(anthropic.NewAssistantMessage(anthropic.NewTextBlock(text)))

	return &Response{
		Text:       text,
		CrewID:     c.ID(),
		CrewMember: c.Name(),
		Verbosity:  c.Verbosity(),
	}, nil
}

package bot

import (
	"context"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"klazomenai/bridge/internal/orchestrator"
)

// Sender abstracts the Matrix message-sending operation.
type Sender interface {
	Send(ctx context.Context, roomID id.RoomID, resp *orchestrator.Response) error
}

// Typer abstracts the Matrix typing indicator operation.
type Typer interface {
	SetTyping(ctx context.Context, roomID id.RoomID, typing bool, timeout time.Duration) error
}

// matrixTyper is the production Typer backed by a real mautrix client.
type matrixTyper struct {
	client *mautrix.Client
}

func (t *matrixTyper) SetTyping(ctx context.Context, roomID id.RoomID, typing bool, timeout time.Duration) error {
	_, err := t.client.UserTyping(ctx, roomID, typing, timeout)
	return err
}

// matrixSender is the production Sender backed by a real mautrix client.
type matrixSender struct {
	client *mautrix.Client
}

func (s *matrixSender) Send(ctx context.Context, roomID id.RoomID, resp *orchestrator.Response) error {
	_, err := s.client.SendMessageEvent(ctx, roomID, event.EventMessage, struct {
		MsgType    event.MessageType `json:"msgtype"`
		Body       string            `json:"body"`
		CrewMember string            `json:"crew_member"`
		Verbosity  string            `json:"verbosity"`
	}{
		MsgType:    event.MsgText,
		Body:       resp.Text,
		CrewMember: resp.CrewID,
		Verbosity:  resp.Verbosity,
	})
	return err
}

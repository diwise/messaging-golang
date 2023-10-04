package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"log/slog"
)

const (
	//PingCommandContentType is the content type for a ping command
	PingCommandContentType = "application/vnd-ping-command"
	//PongResponseContentType is the content type for a pong response
	PongResponseContentType = "application/vnd-pong-response"
)

// PingCommand is a utility command to check the messenger connection
type PingCommand struct {
	Cmd       string    `json:"cmd"`
	Timestamp time.Time `json:"timestamp"`
}

// ContentType returns the content type for a ping command
func (cmd PingCommand) ContentType() string {
	return PingCommandContentType
}

// NewPingCommand instantiates a new ping command
func NewPingCommand() CommandMessage {
	return PingCommand{
		Cmd:       "ping",
		Timestamp: time.Now().UTC(),
	}
}

// NewPingCommandHandler returns a callback function to be called when ping commands
// are received
func NewPingCommandHandler(ctx MsgContext) CommandHandler {
	return func(ctx context.Context, wrapper CommandMessageWrapper, logger *slog.Logger) error {
		var err error

		_, span := tracer.Start(ctx, "ping-pong")
		defer func() {
			if err != nil {
				span.RecordError(err)
			}
			span.End()
		}()

		ping := PingCommand{}
		_ = json.Unmarshal(wrapper.Body(), &ping)

		err = wrapper.RespondWith(wrapper.Context(), NewPongResponse(ping))
		if err != nil {
			err = fmt.Errorf("failed to publish a pong response to ourselves! : %s", err.Error())
			return err
		}

		return nil
	}
}

// PongResponse is a utility response to check the messenger connection
type PongResponse struct {
	Cmd       string `json:"cmd"`
	PingSent  time.Time
	Timestamp time.Time `json:"timestamp"`
}

// ContentType returns the content type for a pong response
func (cmd PongResponse) ContentType() string {
	return PingCommandContentType
}

// NewPongResponse instantiates a new pong response from a ping command
func NewPongResponse(ping PingCommand) CommandMessage {
	return PongResponse{
		Cmd:       "pong",
		PingSent:  ping.Timestamp,
		Timestamp: time.Now().UTC(),
	}
}

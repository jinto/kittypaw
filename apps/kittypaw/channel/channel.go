// Package channel provides messaging channel backends for gopaw.
//
// Each channel is an event producer: it listens for inbound messages
// from a specific platform (Telegram, Slack, Discord, etc.) and emits
// them as core.Event values. Channels also handle sending responses
// back to the originating platform.
package channel

import (
	"context"

	"github.com/jinto/gopaw/core"
)

// Channel is the interface for all messaging channel backends.
// Channels are event producers that emit Events, and can send responses back.
type Channel interface {
	// Start begins listening for messages. Received messages are sent to eventCh.
	// Blocks until ctx is cancelled.
	Start(ctx context.Context, eventCh chan<- core.Event) error

	// SendResponse sends a text response back to the channel.
	SendResponse(ctx context.Context, agentID, response string) error

	// Name returns the channel identifier (e.g., "telegram", "slack").
	Name() string
}

// Package channel provides messaging channel backends for kittypaw.
//
// Each channel is an event producer: it listens for inbound messages
// from a specific platform (Telegram, Slack, Discord, etc.) and emits
// them as core.Event values. Channels also handle sending responses
// back to the originating platform.
package channel

import (
	"context"

	"github.com/jinto/kittypaw/core"
)

// Channel is the interface for all messaging channel backends.
// Channels are event producers that emit Events, and can send responses back.
type Channel interface {
	// Start begins listening for messages. Received messages are sent to eventCh.
	// Blocks until ctx is cancelled.
	Start(ctx context.Context, eventCh chan<- core.Event) error

	// SendResponse sends a text response back to the channel.
	// chatID identifies the destination (e.g., Telegram chat ID, Slack channel ID).
	SendResponse(ctx context.Context, chatID, response string) error

	// Name returns the channel identifier (e.g., "telegram", "slack").
	Name() string
}

// Confirmer is an optional capability for channels that support interactive
// permission dialogs. Channels implement this to enable approval prompts
// for destructive operations (e.g., shell commands, git push).
//
// Use a type assertion to check at runtime:
//
//	if confirmer, ok := ch.(channel.Confirmer); ok { ... }
type Confirmer interface {
	AskConfirmation(ctx context.Context, chatID, description, resource string) (bool, error)
}

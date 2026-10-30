// Package channel is the seam between the pipeline and the services that reach the human. It holds
// the interface, the shared types and the registry, and imports no other internal package.
package channel

import "context"

// Channel is one configured instance of a channel type.
type Channel interface {
	Type() string
	Capabilities() Capabilities
	Validate(options map[string]any) error // per-send options from the config
	Send(ctx context.Context, m Message, s SendSpec) (Receipt, error)
	Resolve(ctx context.Context, dedupKey string, note Message) error // only if Capabilities.Resolve
	Cancel(ctx context.Context, r Receipt) error                      // only if Capabilities.Schedule
}

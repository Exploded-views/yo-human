// Package channeltest is a scriptable fake Channel for tests. It imports only internal/channel.
package channeltest

import (
	"context"
	"sync"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// Fake records what it is given and answers through SendFunc.
type Fake struct {
	Caps     channel.Capabilities
	SendFunc func(ctx context.Context, m channel.Message, s channel.SendSpec) (channel.Receipt, error)

	mu        sync.Mutex
	Sent      []channel.Message
	Specs     []channel.SendSpec
	Resolved  []string
	Cancelled []channel.Receipt
}

var _ channel.Channel = (*Fake)(nil)

// NewFake returns a fake whose sends succeed.
func NewFake(caps channel.Capabilities) *Fake { return &Fake{Caps: caps} }

func (f *Fake) Type() string                       { return "fake" }
func (f *Fake) Capabilities() channel.Capabilities { return f.Caps }
func (f *Fake) Validate(map[string]any) error      { return nil }

// Send records the message and spec, then answers through SendFunc, or succeeds with a receipt
// naming the spec's instance when SendFunc is nil. SendFunc runs outside the lock.
func (f *Fake) Send(ctx context.Context, m channel.Message, s channel.SendSpec) (channel.Receipt, error) {
	f.mu.Lock()
	f.Sent = append(f.Sent, m)
	f.Specs = append(f.Specs, s)
	send := f.SendFunc
	f.mu.Unlock()
	if send != nil {
		return send(ctx, m, s)
	}
	return channel.Receipt{Instance: s.Instance}, nil
}

// Resolve records the dedup key.
func (f *Fake) Resolve(_ context.Context, dedupKey string, _ channel.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Resolved = append(f.Resolved, dedupKey)
	return nil
}

// Cancel records the receipt.
func (f *Fake) Cancel(_ context.Context, r channel.Receipt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cancelled = append(f.Cancelled, r)
	return nil
}

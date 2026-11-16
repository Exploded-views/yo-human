package bus

import "time"

// Visible exposes visible to the external tests.
func (b *Bus) Visible(p Principal, it Item) bool { return b.visible(p, it) }

// SetTimer replaces the long-poll's timeout timer, so a test can fire it by hand.
func (b *Bus) SetTimer(f func(time.Duration) (<-chan time.Time, func())) { b.timer = f }

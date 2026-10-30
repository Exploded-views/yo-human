package bus

// Visible exposes visible to the external tests.
func (b *Bus) Visible(p Principal, it Item) bool { return b.visible(p, it) }

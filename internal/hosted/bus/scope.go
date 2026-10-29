package bus

// visible reports whether p may see it. (stub)
func (b *Bus) visible(p Principal, it Item) bool { return false }

func notFoundErr() *Error { return nil }

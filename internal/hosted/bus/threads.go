package bus

import (
	"context"
	"errors"
)

// Reply adds a message to an item's thread. (stub)
func (b *Bus) Reply(ctx context.Context, p Principal, threadID, body string, sev Severity) (Message, error) {
	return Message{}, errors.New("not implemented")
}

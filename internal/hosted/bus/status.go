package bus

import (
	"context"
	"encoding/json"
)

// PushStatus is not implemented yet.
func (b *Bus) PushStatus(ctx context.Context, p Principal, doc json.RawMessage) error { return errTODO }

// SwarmStatus is not implemented yet.
func (b *Bus) SwarmStatus(ctx context.Context, p Principal, swarm string) ([]StatusDoc, error) {
	return nil, errTODO
}

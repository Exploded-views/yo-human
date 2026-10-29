package bus

import (
	"context"
	"errors"
)

// Post stores d for p. (stub)
func (b *Bus) Post(ctx context.Context, p Principal, d Draft) (PostResult, error) {
	return PostResult{}, errors.New("not implemented")
}

func mapStoreErr(err error) error { return err }

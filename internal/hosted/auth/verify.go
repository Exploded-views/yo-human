package auth

import (
	"context"
	"errors"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// ErrInvalid is the one error Verify gives for a revoked, expired, unknown or malformed token.
var ErrInvalid = errors.New("auth: invalid token")

// TokenStore is the credential lookup Verify needs, by hash only.
type TokenStore interface {
	NodeByHash(h string) (bus.Node, bool, error)
	AccessByHash(h string) (bus.Access, bool, error)
	DeskByHash(h string) (bus.Desk, bool, error)
	StarterByHash(h string) (bus.Starter, bool, error)
	TouchNode(name string, at time.Time) error
}

// Verifier turns a bearer token into a principal.
type Verifier struct{}

// NewVerifier returns a Verifier over s. deskTTL is YH_DESK_TTL.
func NewVerifier(s TokenStore, now func() time.Time, deskTTL time.Duration) *Verifier {
	return &Verifier{}
}

// Verify returns the principal token names and when the credential stops being valid (zero when
// it does not expire).
func (v *Verifier) Verify(ctx context.Context, token string) (bus.Principal, time.Time, error) {
	return bus.Principal{}, time.Time{}, ErrInvalid
}

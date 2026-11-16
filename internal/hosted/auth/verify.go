package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// ErrInvalid is the one error Verify gives for a revoked, expired, unknown or malformed token.
// The caller cannot tell which, so a token cannot be probed for having existed.
var ErrInvalid = errors.New("auth: invalid token")

// TokenStore is the credential lookup Verify needs, by hash only. Each method returns the row
// of its own kind, revoked or not.
type TokenStore interface {
	NodeByHash(h string) (bus.Node, bool, error)
	AccessByHash(h string) (bus.Access, bool, error)
	DeskByHash(h string) (bus.Desk, bool, error)
	StarterByHash(h string) (bus.Starter, bool, error)
	TouchNode(name string, at time.Time) error
}

// touchEvery is how stale a node's last-used time may be before Verify writes a new one, so a
// busy node does not write on every request.
const touchEvery = time.Minute

// Verifier turns a bearer token into a principal.
type Verifier struct {
	store   TokenStore
	now     func() time.Time
	deskTTL time.Duration
}

// NewVerifier returns a Verifier over s, reading time from now. deskTTL is YH_DESK_TTL.
func NewVerifier(s TokenStore, now func() time.Time, deskTTL time.Duration) *Verifier {
	return &Verifier{store: s, now: now, deskTTL: deskTTL}
}

// Verify returns the principal token names and when the credential stops being valid (zero
// when it does not expire). The principal's kind comes from the store that matched, found by
// the token's prefix; a hash is looked up in that one store and compared only as a database
// key, never against another secret. The store has no context in its signature (the plan's),
// so ctx is unused.
func (v *Verifier) Verify(_ context.Context, token string) (bus.Principal, time.Time, error) {
	if !wellFormed(token) {
		return bus.Principal{}, time.Time{}, ErrInvalid
	}
	h := Hash(token)
	var (
		p   bus.Principal
		exp time.Time
		err error
	)
	switch token[:len(PrefixNode)] {
	case PrefixNode:
		p, err = v.node(h)
	case PrefixAccess:
		p, exp, err = v.access(h)
	case PrefixDesk:
		p, exp, err = v.desk(h)
	case PrefixStarter:
		p, err = v.starter(h)
	default: // refresh and executor tokens are not bearer credentials for this verifier
		err = ErrInvalid
	}
	if err != nil {
		return bus.Principal{}, time.Time{}, err
	}
	return p, exp, nil
}

func (v *Verifier) node(h string) (bus.Principal, error) {
	n, ok, err := v.store.NodeByHash(h)
	if err != nil {
		return bus.Principal{}, fmt.Errorf("auth: node lookup: %w", err)
	}
	if !ok || !n.Revoked.IsZero() {
		return bus.Principal{}, ErrInvalid
	}
	if at := v.now(); at.Sub(n.LastUsed) >= touchEvery {
		// The last-used time is a convenience for `token list`: failing to write it must not
		// fail a request that is otherwise good.
		_ = v.store.TouchNode(n.Name, at)
	}
	return bus.Principal{Kind: bus.PrincipalNode, Name: n.Name, Swarm: n.Swarm}, nil
}

func (v *Verifier) access(h string) (bus.Principal, time.Time, error) {
	a, ok, err := v.store.AccessByHash(h)
	if err != nil {
		return bus.Principal{}, time.Time{}, fmt.Errorf("auth: access lookup: %w", err)
	}
	if !ok || !a.Revoked.IsZero() || !v.now().Before(a.Expires) {
		return bus.Principal{}, time.Time{}, ErrInvalid
	}
	return bus.Principal{Kind: bus.PrincipalDuet, Name: "duet"}, a.Expires, nil
}

func (v *Verifier) desk(h string) (bus.Principal, time.Time, error) {
	d, ok, err := v.store.DeskByHash(h)
	if err != nil {
		return bus.Principal{}, time.Time{}, fmt.Errorf("auth: desk lookup: %w", err)
	}
	exp := d.Created.Add(v.deskTTL)
	if !ok || !d.Revoked.IsZero() || !v.now().Before(exp) {
		return bus.Principal{}, time.Time{}, ErrInvalid
	}
	return bus.Principal{Kind: bus.PrincipalDesk, Name: "desk"}, exp, nil
}

func (v *Verifier) starter(h string) (bus.Principal, error) {
	s, ok, err := v.store.StarterByHash(h)
	if err != nil {
		return bus.Principal{}, fmt.Errorf("auth: starter lookup: %w", err)
	}
	if !ok || !s.Revoked.IsZero() {
		return bus.Principal{}, ErrInvalid
	}
	return bus.Principal{Kind: bus.PrincipalDeskStart, Name: s.Name}, nil
}

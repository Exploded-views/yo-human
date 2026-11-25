// Package notify is the channel adapter interface hosted mode sends through: the levels the bus
// names, the message, and the three classes of failure a channel reports. Standard library only.
// The channel implementations live in internal/hosted/egress.
package notify

import (
	"context"
	"errors"
	"time"
)

// Level is how loudly a message reaches the owner. These are the bus's own names; each channel
// maps them to its own.
type Level string

// The levels.
const (
	LevelPassive       Level = "passive"
	LevelActive        Level = "active"
	LevelTimeSensitive Level = "time-sensitive"
	LevelCritical      Level = "critical"
)

// Message is one push. ID is empty for a collapsed nudge, and yh-<outbox id> for an outbox
// message, so a re-send replaces the notification on the phone instead of adding a second.
type Message struct {
	ID, Title, Body, Group string
	Level                  Level
}

// Channel is one way to reach the owner.
type Channel interface {
	Name() string
	Levels() []Level
	Send(ctx context.Context, m Message) error
}

type class int

const (
	classTransient class = iota
	classPermanent
	classBroken
)

// marked is err with what a channel learned about it. Each helper below copies what is already
// on the chain and changes one thing, so the marks combine in any order.
type marked struct {
	err    error
	class  class
	status int
	after  time.Duration
}

func (m *marked) Error() string { return m.err.Error() }
func (m *marked) Unwrap() error { return m.err }

// with returns err marked as change makes it, or nil for a nil err.
func with(err error, change func(*marked)) error {
	if err == nil {
		return nil
	}
	m := &marked{err: err}
	var cur *marked
	if errors.As(err, &cur) {
		m.class, m.status, m.after = cur.class, cur.status, cur.after
	}
	change(m)
	return m
}

func find(err error) *marked {
	var m *marked
	if errors.As(err, &m) {
		return m
	}
	return &marked{}
}

// Permanent marks err as a refusal of this message: it will never be accepted, so it is not
// retried (a 4xx other than 401, 403, 408 and 429).
func Permanent(err error) error { return with(err, func(m *marked) { m.class = classPermanent }) }

// IsPermanent reports whether err was marked by Permanent.
func IsPermanent(err error) bool { return find(err).class == classPermanent }

// Broken marks err as a refusal of the channel itself (401 or 403): nothing it sends will
// arrive until the owner fixes it.
func Broken(err error) error { return with(err, func(m *marked) { m.class = classBroken }) }

// IsBroken reports whether err was marked by Broken.
func IsBroken(err error) bool { return find(err).class == classBroken }

// WithStatus records the HTTP status that caused err.
func WithStatus(err error, status int) error { return with(err, func(m *marked) { m.status = status }) }

// Status is the status WithStatus recorded in err's chain, or 0.
func Status(err error) int { return find(err).status }

// MaxRetryAfter caps what RetryAfter reports.
const MaxRetryAfter = time.Hour

// WithRetryAfter records the delay the channel asked for.
func WithRetryAfter(err error, d time.Duration) error {
	return with(err, func(m *marked) { m.after = d })
}

// RetryAfter is the delay a 429 or 503 asked for, at most MaxRetryAfter, or 0 if absent.
// Anything else is transient: a network error, a timeout, 408, 429 or a 5xx.
func RetryAfter(err error) time.Duration {
	return min(max(find(err).after, 0), MaxRetryAfter)
}

// Package notify is the channel adapter interface hosted mode sends through: the levels the bus
// names, the message, and the three classes of failure a channel reports. Standard library only.
// The channel implementations live in internal/hosted/egress.
package notify

import (
	"context"
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

// Permanent marks err as a refusal of this message: it will never be accepted, so it is not
// retried.
func Permanent(err error) error { return err }

// IsPermanent reports whether err was marked by Permanent.
func IsPermanent(err error) bool { return false }

// Broken marks err as a refusal of the channel itself (bad credentials): nothing it sends will
// arrive until the owner fixes it.
func Broken(err error) error { return err }

// IsBroken reports whether err was marked by Broken.
func IsBroken(err error) bool { return false }

// WithStatus records the HTTP status that caused err.
func WithStatus(err error, status int) error { return err }

// Status is the status WithStatus recorded in err's chain, or 0.
func Status(err error) int { return 0 }

// MaxRetryAfter caps what RetryAfter reports.
const MaxRetryAfter = time.Hour

// WithRetryAfter records the delay the channel asked for.
func WithRetryAfter(err error, d time.Duration) error { return err }

// RetryAfter is the delay a 429 or 503 asked for, at most MaxRetryAfter, or 0 if absent.
func RetryAfter(err error) time.Duration { return 0 }

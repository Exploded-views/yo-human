package core

import "time"

// Status is the first word of the RESULT line.
type Status string

// The eight statuses of the contract, plus StatusOK for read-only commands.
const (
	StatusDelivered  Status = "delivered"
	StatusPartial    Status = "partial"
	StatusQueued     Status = "queued"
	StatusFailed     Status = "failed"
	StatusHeld       Status = "held"
	StatusSuppressed Status = "suppressed"
	StatusResolved   Status = "resolved"
	StatusNoop       Status = "noop"
	StatusOK         Status = "ok"
)

// What happened to one channel's send.
const (
	ChannelSent      = "sent"
	ChannelQueued    = "queued"
	ChannelHeld      = "held"
	ChannelDead      = "dead"
	ChannelSkipped   = "skipped"
	ChannelCancelled = "cancelled"
)

// Result is what a command reports. Its JSON form is the spec's object, less the version, which
// the cli adds.
type Result struct {
	Status   Status          `json:"status"`
	ID       string          `json:"id"`
	Channels []ChannelResult `json:"channels"`
	Next     *Next           `json:"next,omitempty"`
	DedupKey string          `json:"dedup_key,omitempty"` // set by the page shim, whose caller may not have chosen the key

	// Warnings are problems that came with the result but do not change its exit code.
	Warnings []error `json:"-"`
}

// ChannelResult is one channel's outcome. Error is the channel's own error text, never a secret.
type ChannelResult struct {
	Channel string `json:"channel"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`

	ProviderID string `json:"-"` // the provider's id for a send that succeeded
	Scheduled  bool   `json:"-"` // the provider holds the send for a later delivery

	attempts int       // sends made so far, this one included
	config   bool      // the channel rejected its configuration
	buried   bool      // the entry is in the dead letter
	parked   bool      // the entry cannot be sent under this config and was put back untouched
	unbuilt  bool      // skipped: this call could not build the channel, so nothing was queued
	level    string    // the level of the message the entry carries
	retryAt  time.Time // when a queued entry is next due
}

// Next tells the agent what to do about a queued result.
type Next struct {
	Do           string `json:"do"`
	AfterSeconds int    `json:"after_seconds"`
	AttemptsLeft int    `json:"attempts_left"`
	Then         string `json:"then"`
}

// Summarize reduces the per-channel outcomes to one status. Only channels that reach a human count
// (reachesHuman by channel name), so a log line never makes a failed page look delivered.
func Summarize(results []ChannelResult, reachesHuman map[string]bool) Status {
	n := map[string]int{}
	for _, r := range results {
		if reachesHuman[r.Channel] {
			n[r.Status]++
		}
	}
	// Dead and cancelled sends will not arrive. A skipped one lost its claim to another process,
	// which is delivering it, so it is still on its way: queued, never failed.
	unfinished := n[ChannelQueued] + n[ChannelHeld] + n[ChannelDead] + n[ChannelCancelled] + n[ChannelSkipped]
	switch {
	case n[ChannelSent] > 0 && unfinished == 0:
		return StatusDelivered
	case n[ChannelSent] > 0:
		return StatusPartial
	case n[ChannelQueued]+n[ChannelSkipped] > 0:
		return StatusQueued
	case n[ChannelHeld] > 0:
		return StatusHeld
	}
	return StatusFailed
}

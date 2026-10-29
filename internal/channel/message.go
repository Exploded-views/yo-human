package channel

import (
	"fmt"
	"time"
)

// Interruption is how loudly a channel should deliver a message.
type Interruption string

const (
	Passive       Interruption = "passive"
	Active        Interruption = "active"
	TimeSensitive Interruption = "time-sensitive"
	Critical      Interruption = "critical"
)

// ParseInterruption turns a config value into an Interruption.
func ParseInterruption(s string) (Interruption, error) {
	switch i := Interruption(s); i {
	case Passive, Active, TimeSensitive, Critical:
		return i, nil
	}
	return "", fmt.Errorf("unknown interruption %q; valid: %s, %s, %s, %s", s, Passive, Active, TimeSensitive, Critical)
}

// Action is a labelled link on a message.
type Action struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Message is what the agent asked to send.
type Message struct {
	ID       string    `json:"id"`
	Level    string    `json:"level"`
	Title    string    `json:"title"`
	Ask      string    `json:"ask"`
	Body     string    `json:"body"`
	Link     string    `json:"link"`
	Project  string    `json:"project"`
	Agent    string    `json:"agent"`
	DedupKey string    `json:"dedup_key"`
	Actions  []Action  `json:"actions"`
	Created  time.Time `json:"created"`
	Resolved bool      `json:"resolved"`
}

// Capabilities is what a channel type can do; the pipeline degrades a message to fit.
type Capabilities struct {
	Interruptions     map[Interruption]bool // passive, active, time-sensitive, critical
	Markdown          bool
	Link              bool
	MaxActions        int  // 0 = none
	Subtitle          bool // a separate line for the ask
	NativeDedup       bool // the provider updates an existing alert by key
	Resolve           bool // the provider can close an incident
	Schedule          bool // the provider can deliver at a later time, and cancel it
	ReachesHuman      bool // false for log-like types; they never count as delivery
	MaxTitle, MaxBody int
}

// SendSpec is the per-send detail a channel receives beside the message.
type SendSpec struct {
	Instance     string
	Interruption Interruption
	DeliverAt    time.Time
	Options      map[string]any
}

// Receipt is what a channel returns for an accepted send.
type Receipt struct {
	Instance, ProviderID string
	Scheduled            bool
}

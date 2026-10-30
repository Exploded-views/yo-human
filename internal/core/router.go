// Package core turns a level name into planned sends. It makes no network calls.
package core

import (
	"maps"
	"strings"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
)

// PlannedSend is one send a level calls for, with when it may be delivered.
type PlannedSend struct {
	Instance     string
	Interruption channel.Interruption
	Options      map[string]any
	DeliverAt    time.Time // zero when the send is not deferred
	HoldLocally  bool      // true when the queue must hold it until DeliverAt

	// Pinned makes the entry carry Fingerprint, not the current instance's: a resolve note goes to
	// the destination the incident reached, whatever the config says now.
	Pinned      bool
	Fingerprint string
}

// Plan expands level into its sends. For a level that holds in quiet hours, inside a window each
// send is deferred to the window's end: by the channel when its capabilities say it can schedule,
// otherwise by the queue (HoldLocally). A channel missing from caps is held locally. An unknown level
// is a usage problem that lists every level, quietest first.
func Plan(c *config.Config, level string, now time.Time, caps map[string]channel.Capabilities) ([]PlannedSend, error) {
	lv, ok := find(c, level)
	if !ok {
		return nil, unknownLevel(c, level)
	}
	var deliverAt time.Time
	if lv.HoldInQuietHours {
		if end, inside := QuietEnd(c.QuietHours, now); inside {
			deliverAt = end
		}
	}
	out := make([]PlannedSend, 0, len(lv.Send))
	for _, s := range lv.Send {
		p := PlannedSend{Instance: s.Channel, Interruption: s.Interruption, Options: maps.Clone(s.Options), DeliverAt: deliverAt}
		p.HoldLocally = !deliverAt.IsZero() && !caps[s.Channel].Schedule
		out = append(out, p)
	}
	return out, nil
}

func find(c *config.Config, name string) (config.Level, bool) {
	for _, l := range c.Levels {
		if l.Name == name {
			return l, true
		}
	}
	return config.Level{}, false
}

func unknownLevel(c *config.Config, level string) error {
	names := make([]string, 0, len(c.Levels))
	for _, l := range c.Levels {
		names = append(names, l.Name+" (use when "+l.UseWhen+")")
	}
	return problem.Usagef("unknown level %q", "levels, quietest first: "+strings.Join(names, ", "),
		`retry with --level <name>; run "yo-human levels --json" to read them`, problem.Clip(level))
}

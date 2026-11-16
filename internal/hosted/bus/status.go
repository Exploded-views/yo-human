package bus

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// The status document's limits. MaxStatus is the whole document in bytes.
const (
	MaxStatus        = 16 << 10
	maxStatusEntries = 64
)

var (
	statusKeys   = []string{"main_health", "sessions", "spend", "swarm", "ts", "waiting_on_human"}
	sessionState = []string{"working", "blocked", "idle", "done"}
	tokenRE      = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	handleIDRE   = regexp.MustCompile(`^ob-[1-9][0-9]{0,5}$`)
)

// statusDoc is a status document as the bus stores it, re-encoded from these fields: whatever
// else the sender wrote, in whatever order, does not survive.
type statusDoc struct {
	Swarm          string          `json:"swarm"`
	Sessions       []statusSession `json:"sessions"`
	WaitingOnHuman []string        `json:"waiting_on_human"`
	MainHealth     string          `json:"main_health"`
	Spend          float64         `json:"spend"`
	TS             int64           `json:"ts"`
}

type statusSession struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// PushStatus stores a node's status document for its own swarm, replacing the last, and
// appends a status_pushed event. The document is an object of at most MaxStatus bytes with
// exactly the fields swarm (the node's own), sessions[]{name, state}, waiting_on_human[],
// main_health, spend and ts. Free text is data; in a walled swarm session names and entries are
// typed ids only.
func (b *Bus) PushStatus(ctx context.Context, p Principal, doc json.RawMessage) error {
	if e := needNode(p, "push your swarm's status"); e != nil {
		return e
	}
	d, perr := parseStatus(doc)
	if perr != nil {
		return perr
	}
	if d.Swarm != p.Swarm {
		return invalid("swarm", "your own swarm's name")
	}
	if e := d.check(b.isWalled(p.Swarm)); e != nil {
		return e
	}
	stored, _ := json.Marshal(d) // cannot fail: strings, integers and a finite float
	err := b.s.Tx(ctx, func(x Tx) error {
		now := b.now().UTC()
		if err := x.PutStatus(StatusDoc{Swarm: p.Swarm, Doc: stored, Updated: now}); err != nil {
			return err
		}
		_, err := x.AppendEvent(Event{Swarm: p.Swarm, Type: EventStatusPushed, Created: now})
		return err
	})
	if err != nil {
		return mapStoreErr(err)
	}
	b.committed(p.Swarm)
	return nil
}

// SwarmStatus is the latest status document of one swarm, or of every swarm for "". Duet only.
func (b *Bus) SwarmStatus(ctx context.Context, p Principal, swarm string) ([]StatusDoc, error) {
	if e := forbiddenUnlessDuet(p); e != nil {
		return nil, e
	}
	if swarm != "" && !ValidSwarm(swarm) {
		return nil, invalid("swarm", "a swarm name, or empty for every swarm")
	}
	var out []StatusDoc
	err := b.s.Tx(ctx, func(x Tx) (err error) {
		out, err = x.Statuses(swarm)
		return err
	})
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return out, nil
}

// parseStatus reads doc strictly: one JSON object, within the size limit, with every one of the
// document's fields (a missing one is refused here, an extra one by the decoder) and the right types.
func parseStatus(doc json.RawMessage) (statusDoc, *Error) {
	bad := func() (statusDoc, *Error) {
		return statusDoc{}, invalid("status", "one JSON object of at most 16 KiB with exactly the fields swarm, sessions, waiting_on_human, main_health, spend and ts")
	}
	if len(doc) > MaxStatus {
		return bad()
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(doc, &keys); err != nil || keys == nil {
		return bad()
	}
	for _, k := range statusKeys {
		if _, ok := keys[k]; !ok {
			return bad()
		}
	}
	var d statusDoc
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil || d.Sessions == nil || d.WaitingOnHuman == nil {
		return bad()
	}
	return d, nil
}

func (d statusDoc) check(walled bool) *Error {
	if e := d.checkLists(walled); e != nil {
		return e
	}
	switch {
	case !tokenRE.MatchString(d.MainHealth):
		return invalid("main_health", "a token of 1 to 40 characters from a-z, 0-9 and -")
	case d.Spend < 0:
		return invalid("spend", "a number of zero or more")
	case d.TS < 0:
		return invalid("ts", "a number of seconds since the epoch, zero or more")
	}
	return nil
}

func (d statusDoc) checkLists(walled bool) *Error {
	if len(d.Sessions) > maxStatusEntries {
		return invalid("sessions", "at most 64 sessions")
	}
	if len(d.WaitingOnHuman) > maxStatusEntries {
		return invalid("waiting_on_human", "at most 64 entries")
	}
	for _, s := range d.Sessions {
		if !slices.Contains(sessionState, s.State) {
			return invalid("sessions.state", "working, blocked, idle or done")
		}
		if !statusText(s.Name, walled, tokenRE) {
			return invalid("sessions.name", statusTextRule(walled, "a name of 1 to 40 characters from a-z, 0-9 and -"))
		}
	}
	for _, w := range d.WaitingOnHuman {
		if !statusText(w, walled, handleIDRE) {
			return invalid("waiting_on_human", statusTextRule(walled, "an id of the form ob-<n>, n from 1 to 999999"))
		}
	}
	return nil
}

// statusText: walled swarms' entries must match the typed pattern; other swarms' are text of 1 to
// MaxHandle characters with no refused character.
func statusText(s string, walled bool, typed *regexp.Regexp) bool {
	if walled {
		return typed.MatchString(s)
	}
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= MaxHandle && utf8.ValidString(s) && !strings.ContainsFunc(s, isRefusedRune)
}

func statusTextRule(walled bool, typed string) string {
	if walled {
		return typed
	}
	return "text of 1 to 80 characters with no control or format characters"
}

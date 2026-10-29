package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/store"
)

// Decision is what a notify does about its dedup key's incident.
type Decision int

// The decisions, in the order Decide weighs them.
const (
	SendNew      Decision = iota // no open incident: send to every planned channel
	SendRaised                   // louder than the incident: send, and raise its level
	UpdateNative                 // same level, changed content: only channels with native dedup
	Suppress                     // record it and send nothing
	Remind                       // the reminder interval has passed: send again
)

// Decide weighs a repeat of an incident against what is recorded. An incident that is missing,
// resolved, or silent for longer than ttl starts afresh. Otherwise a louder repeat is sent, a repeat
// whose reminder interval has passed is sent again, a quieter one is suppressed, and one at the same
// level is an update when its content changed and suppressed when not. A reminder of zero never fires.
func Decide(inc *store.Incident, rank int, contentHash string, reminder, ttl time.Duration, now time.Time) Decision {
	if inc == nil || !inc.Open {
		return SendNew
	}
	since := now.Sub(inc.LastSent)
	switch {
	case since > ttl:
		return SendNew
	case rank > inc.Rank:
		return SendRaised
	case reminder > 0 && since >= reminder:
		return Remind
	case rank < inc.Rank:
		return Suppress
	case contentHash != inc.ContentHash:
		return UpdateNative
	}
	return Suppress
}

// ContentHash is the sha256 of what a human reads in a message: title, ask, body, link and actions.
// Every part is length-prefixed, so text moved from one field to the next changes the hash.
func ContentHash(m channel.Message) string {
	h := sha256.New()
	put := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte{':'})
		h.Write([]byte(s))
	}
	for _, s := range []string{m.Title, m.Ask, m.Body, m.Link} {
		put(s)
	}
	put(strconv.Itoa(len(m.Actions)))
	for _, a := range m.Actions {
		put(a.Label)
		put(a.URL)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// admission is what the incident check lets through: the sends to make and where they are queued.
type admission struct {
	plan    []PlannedSend
	entries []entry
	cancel  []heldSend // sends a channel holds that this call replaced: an expired generation's, or a quieter copy's
	skipped []string   // planned instances this call could not build: neither sent nor queued
	dropped bool       // a waiting message whose incident was resolved after it was queued
}

// splitBuilt splits a plan into the sends whose channel this call built and the instances it could
// not. A send for a channel that could not be built is skipped, not queued: the command reports why,
// and a page queued for it would reach the human long after they fixed the config.
func (p *Pipeline) splitBuilt(plan []PlannedSend) (built []PlannedSend, skipped []string) {
	for _, s := range plan {
		if _, ok := p.d.Channels[s.Instance]; ok {
			built = append(built, s)
		} else {
			skipped = append(skipped, s.Instance)
		}
	}
	return built, skipped
}

// admit decides what m does to its incident, records that, and enqueues the sends it calls for, all
// under one hold of the incident lock: two concurrent repeats cannot both page, and a resolve that
// follows finds every queued send of the incident to cancel. Only local file writes happen under the
// lock, never a network call. A message without a dedup key is not an incident and takes no lock.
func (p *Pipeline) admit(m channel.Message, plan []PlannedSend, now, queuedAt time.Time) (admission, error) {
	if m.DedupKey == "" {
		a := admission{}
		a.plan, a.skipped = p.splitBuilt(plan)
		var err error
		a.entries, err = p.enqueue(m, 0, a.plan)
		return a, err
	}
	var a admission
	id := incidentID(m)
	err := p.d.Store.WithState(func(st *store.State) error {
		incs := st.Incidents
		p.pruneClosed(incs, now)
		if resolvedBefore(incs[id], admitKey(m), queuedAt) {
			a.dropped = true // a resolve came first: it must not page now
			return nil
		}
		rank, _ := p.d.Config.Rank(m.Level)
		hash := ContentHash(m)
		dec, err := p.decide(incs[id], m.Level, rank, hash, now)
		if err != nil {
			return err
		}
		cancel := record(st, m, rank, hash, dec, now)
		if dec == SendRaised || dec == Remind { // the copies still held are replaced by this send
			if _, err := p.d.Store.CancelHeld(id, now); err != nil {
				return err
			}
		}
		a = admission{cancel: cancel}
		a.plan, a.skipped = p.splitBuilt(p.planFor(dec, plan))
		a.entries, err = p.enqueue(m, incs[id].Generation, a.plan)
		return err
	})
	return a, err
}

// resolvedBefore reports a waiting message (queuedAt set; zero for a message admitted as it is
// sent) that a resolve of its key came before. The first rule needs no clock: the resolve listed it
// on the record (Withdrawn), as it found every waiting message of the key queued or in flight while
// it held the lock. The second compares times, for a message the listing could not see: a waiting
// message is written exactly when the lock is busy, so one can land while a resolve holds the lock,
// after its listing. It is dropped if the incident closed after it was queued; on a tie, or a clock
// that stepped back, it is checked and sent, as a message after the resolve would be.
func resolvedBefore(inc *store.Incident, entryKey string, queuedAt time.Time) bool {
	switch {
	case queuedAt.IsZero() || inc == nil:
		return false
	case slices.Contains(inc.Withdrawn, entryKey):
		return true
	}
	return !inc.Open && inc.Updated.After(queuedAt)
}

// decide is Decide, with a repeat of a stranded incident sent again rather than suppressed.
func (p *Pipeline) decide(inc *store.Incident, level string, rank int, hash string, now time.Time) (Decision, error) {
	lv, _ := find(p.d.Config, level)
	dec := Decide(inc, rank, hash, lv.Reminder, p.d.Config.IncidentTTL, now)
	if dec != Suppress && dec != UpdateNative {
		return dec, nil
	}
	stranded, err := p.stranded(inc)
	if err != nil || !stranded {
		return dec, err
	}
	return Remind, nil
}

// stranded reports an open incident no human has been told about and nothing is left to tell: no
// send of it was delivered or scheduled on a channel that reaches a human, and none is queued or in
// flight (each was dead-lettered). Suppressing a repeat of it would leave the human unreached until
// the incident expires, so the repeat is sent again in the same generation.
func (p *Pipeline) stranded(inc *store.Incident) (bool, error) {
	for _, r := range inc.Reached {
		if p.d.Caps[r.Instance].ReachesHuman && (len(r.Delivered) > 0 || len(r.Scheduled) > 0) {
			return false, nil
		}
	}
	pending, err := p.d.Store.HasPending(IncidentKey(inc.Project, inc.Key))
	return !pending, err
}

// planFor is the part of plan the decision sends: all of it, none of it, or for an update the
// channels that update an existing alert by key.
func (p *Pipeline) planFor(dec Decision, plan []PlannedSend) []PlannedSend {
	switch dec {
	case Suppress:
		return nil
	case UpdateNative:
		var out []PlannedSend
		for _, s := range plan {
			if p.d.Caps[s.Instance].NativeDedup {
				out = append(out, s)
			}
		}
		return out
	}
	return plan
}

// record writes the decision into the incident map. LastSent moves only when a human may be paged.
// It returns the sends a channel holds that this message replaces, for the caller to cancel: those
// of a generation that expired, or, for a louder repeat or a reminder, the copies of this generation
// still scheduled, which it takes off the record (the caller removes those held in the queue).
func record(st *store.State, m channel.Message, rank int, hash string, dec Decision, now time.Time) []heldSend {
	incs := st.Incidents
	id := incidentID(m)
	if dec == SendNew {
		gen, prev := st.NewGeneration(), ""
		var held []heldSend
		if old := incs[id]; old != nil {
			prev = endOf(old)
			if old.Open { // expired: a resolve has not cancelled its held sends
				held = heldSends(old)
			}
		}
		var withdrawn []string
		if old := incs[id]; old != nil {
			withdrawn = old.Withdrawn // a message the resolve withdrew may still be waiting
		}
		incs[id] = &store.Incident{Key: m.DedupKey, Project: m.Project, Generation: gen, Previous: prev, Level: m.Level, Rank: rank, Open: true,
			ContentHash: hash, Latest: m.Title, Opened: now, LastSent: now, Updated: now, Withdrawn: withdrawn}
		return held
	}
	inc := incs[id]
	inc.Latest, inc.Updated = m.Title, now
	switch dec {
	case SendRaised:
		inc.Level, inc.Rank, inc.ContentHash, inc.LastSent = m.Level, rank, hash, now
		return replaceScheduled(inc)
	case Remind:
		inc.ContentHash, inc.LastSent = hash, now
		return replaceScheduled(inc)
	case UpdateNative:
		inc.ContentHash = hash
	}
	return nil
}

// replaceScheduled takes every send a channel still holds off the incident and returns them to be
// cancelled: the message that replaces them goes out now, or is held in their place.
func replaceScheduled(inc *store.Incident) []heldSend {
	held := heldSends(inc)
	for i := range inc.Reached {
		inc.Reached[i].Scheduled = nil
	}
	return held
}

// heldSend is a send a channel holds for later delivery, with the destination it was sent through
// and the generation of the incident it belongs to.
type heldSend struct {
	channel.Receipt
	Fingerprint string
	Generation  int
	Level       string // the incident's level, whose retry a queued cancel follows
}

// heldSends lists every send the incident's channels hold for later delivery.
func heldSends(inc *store.Incident) []heldSend {
	var out []heldSend
	for _, r := range inc.Reached {
		for _, sc := range r.Scheduled {
			out = append(out, heldSend{channel.Receipt{Instance: r.Instance, ProviderID: sc.ID, Scheduled: true}, sc.Fingerprint, inc.Generation, inc.Level})
		}
	}
	return out
}

// endOf is how a generation ends when its key opens afresh: an incident still open has expired.
func endOf(old *store.Incident) string {
	if old.Open {
		return "expired"
	}
	return "resolved"
}

// outdated says why a queued entry no longer belongs to its incident, or "" if it still does. The
// incident was resolved, or has been reopened since the entry was written (after a resolve, or
// after the old one expired and was superseded). A resolve note is the one entry meant for a closed
// incident, and is outdated only once the incident is reopened. No record means the entry is kept.
func outdated(pl payload, inc *store.Incident) string {
	switch {
	case inc == nil:
		return ""
	case inc.Generation != pl.Generation && inc.Previous == "expired":
		return "expired (superseded)"
	case inc.Generation != pl.Generation:
		return "reopened"
	case !pl.Message.Resolved && !inc.Open:
		return "resolved"
	}
	return ""
}

// stale says why the entry is outdated against the incident as it stands now, or "".
func (p *Pipeline) stale(pl payload) (string, error) {
	key := pl.Message.DedupKey
	if key == "" {
		return "", nil
	}
	cause := ""
	err := p.d.Store.WithIncidents(func(incs map[string]*store.Incident) error {
		cause = outdated(pl, incs[incidentID(pl.Message)])
		return nil
	})
	return cause, err
}

// landing is what a send that arrived after its incident closed leaves to do, once the lock is
// released: a scheduled delivery to cancel, or a resolve note to send.
type landing struct {
	cancel *heldSend
	note   *store.Pending
}

// landed records that a send to res.Channel arrived, so a resolve can address that channel. A held
// send is scheduled for later and fires unless cancelled: if its generation is over, or its
// incident has been resolved, it is to be cancelled once the lock is released. A delivered message
// is recorded on the incident as it stands (an older generation's page is still seen by the human);
// if that incident is closed, a resolve note for it is queued in the same hold of the lock. The
// sent entry is removed by complete inside that hold, or after it if the lock could not be taken.
func (p *Pipeline) landed(pl payload, res ChannelResult, complete func() error) (landing, error) {
	var l landing
	var cerr error
	done := false
	key := incidentID(pl.Message)
	if p.beforeLand != nil {
		p.beforeLand()
	}
	// The send is done: its record must not be dropped for a busy lock, so this wait is not bounded
	// by the deadline. The critical section is a few local file writes.
	err := p.d.Store.Unbounded().WithIncidents(func(incs map[string]*store.Incident) error {
		cerr, done = complete(), true
		inc := incs[key]
		switch {
		case inc == nil:
			return nil
		case res.Scheduled && (inc.Generation != pl.Generation || !inc.Open):
			l.cancel = &heldSend{channel.Receipt{Instance: res.Channel, ProviderID: res.ProviderID, Scheduled: true}, pl.Fingerprint, pl.Generation, pl.Message.Level}
			return nil
		case inc.Open:
			inc.Reached = addReached(inc.Reached, res, pl.Fingerprint)
			return nil
		}
		entries, err := p.enqueue(resolveNote(*inc, p.d.NewID(), "", p.d.Now()), inc.Generation,
			[]PlannedSend{{Instance: res.Channel, Interruption: p.quietest(res.Channel), Pinned: true, Fingerprint: pl.Fingerprint}})
		if err == nil {
			l.note = &entries[0].pending
		}
		return err
	})
	if !done { // the lock could not be taken: the entry is still settled
		cerr = complete()
	}
	return l, errors.Join(cerr, err)
}

// addReached merges a receipt into the incident's per-instance records: every destination that
// delivered, and every provider id the instance holds for later, with the destination it went through.
func addReached(rs []store.Reached, res ChannelResult, fingerprint string) []store.Reached {
	for i := range rs {
		if rs[i].Instance != res.Channel {
			continue
		}
		if !res.Scheduled {
			if !slices.Contains(rs[i].Delivered, fingerprint) {
				rs[i].Delivered = append(rs[i].Delivered, fingerprint)
			}
		} else if !slices.ContainsFunc(rs[i].Scheduled, func(s store.ScheduledSend) bool { return s.ID == res.ProviderID }) {
			rs[i].Scheduled = append(rs[i].Scheduled, store.ScheduledSend{ID: res.ProviderID, Fingerprint: fingerprint})
		}
		return rs
	}
	r := store.Reached{Instance: res.Channel}
	if res.Scheduled {
		r.Scheduled = []store.ScheduledSend{{ID: res.ProviderID, Fingerprint: fingerprint}}
	} else {
		r.Delivered = []string{fingerprint}
	}
	return append(rs, r)
}

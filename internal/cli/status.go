package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
)

type queuedView struct {
	ID          string    `json:"id"`
	Channel     string    `json:"channel"`
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	Reason      string    `json:"reason,omitempty"` // why the entry is parked, if it is
	Kind        string    `json:"kind"`
}

type heldView struct {
	ID        string    `json:"id"`
	Channel   string    `json:"channel"`
	DeliverAt time.Time `json:"deliver_at"`
	Kind      string    `json:"kind"`
}

type deadView struct {
	ID      string    `json:"id"`
	Channel string    `json:"channel"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
}

type incidentView struct {
	Key        string    `json:"key"`
	Project    string    `json:"project"`
	Level      string    `json:"level"`
	Generation int       `json:"generation"`
	Opened     time.Time `json:"opened"`
	LastSent   time.Time `json:"last_sent"`
	Latest     string    `json:"latest"`
}

// snapshot is what status reports: the queue split into due-or-retrying and held, the dead letter,
// and the incidents still open.
type snapshot struct {
	Queued    []queuedView   `json:"queued"`
	Held      []heldView     `json:"held"`
	Dead      []deadView     `json:"dead"`
	Incidents []incidentView `json:"incidents"`
	Inflight  int            `json:"inflight"`
}

// statusUsage is the valid text of every status usage error.
const statusUsage = `yo-human status [--project <name>] [--all]`

// scope says which projects status shows: one, or every one. An entry of no project (the
// cancellation of a provider's scheduled send, an entry that cannot be read) is shown to all.
type scope struct {
	all     bool
	project string
}

func (sc scope) shows(project string) bool { return sc.all || project == "" || project == sc.project }

// takeSnapshot reads the state at at, as far as it can, and keeps what sc shows. It builds no
// channel and writes nothing.
func takeSnapshot(cfg *config.Config, st *core.State, at time.Time, sc scope) (snapshot, error) {
	all, err := st.Snapshot(cfg, at)
	snap := snapshot{Queued: []queuedView{}, Held: []heldView{}, Dead: []deadView{}, Incidents: []incidentView{}, Inflight: all.Inflight}
	for _, q := range all.Queue {
		switch {
		case !sc.shows(q.Project):
		case q.DeliverAt.After(at):
			snap.Held = append(snap.Held, heldView{q.ID, q.Instance, q.DeliverAt.UTC(), q.Kind})
		default:
			snap.Queued = append(snap.Queued, queuedView{q.ID, q.Instance, q.Attempts, q.NextAttempt.UTC(), q.Reason, q.Kind})
		}
	}
	for _, d := range all.Dead {
		if sc.shows(d.Project) {
			snap.Dead = append(snap.Dead, deadView{d.ID, d.Instance, d.Reason, d.At.UTC(), d.Kind})
		}
	}
	for _, i := range all.Incidents {
		if sc.shows(i.Project) {
			snap.Incidents = append(snap.Incidents, incidentView{i.Key, i.Project, i.Level, i.Generation, i.Opened.UTC(), i.LastSent.UTC(), i.Latest})
		}
	}
	return snap, err
}

// lines renders the snapshot as text. Every string that came from a message or a channel passes
// through oneLine, so none can forge a line.
func (s snapshot) lines() []string {
	out := []string{
		fmt.Sprintf("queued: %d", len(s.Queued)), fmt.Sprintf("held: %d", len(s.Held)), fmt.Sprintf("dead: %d", len(s.Dead)),
		fmt.Sprintf("inflight: %d", s.Inflight), fmt.Sprintf("incidents: %d", len(s.Incidents)),
	}
	for _, q := range s.Queued {
		line := fmt.Sprintf("queued %s %s attempts=%d next=%s kind=%s", q.ID, oneLine(q.Channel), q.Attempts, q.NextAttempt.Format(time.RFC3339), q.Kind)
		if q.Reason != "" {
			line += " reason: " + oneLine(q.Reason)
		}
		out = append(out, line)
	}
	for _, h := range s.Held {
		out = append(out, fmt.Sprintf("held %s %s until=%s kind=%s", h.ID, oneLine(h.Channel), h.DeliverAt.Format(time.RFC3339), h.Kind))
	}
	for _, d := range s.Dead {
		out = append(out, fmt.Sprintf("dead %s %s kind=%s: %s", d.ID, oneLine(d.Channel), d.Kind, oneLine(d.Reason)))
	}
	for _, i := range s.Incidents {
		out = append(out, fmt.Sprintf("incident %s project=%s level=%s generation=%d last_sent=%s latest: %s",
			oneLine(i.Key), oneLine(i.Project), oneLine(i.Level), i.Generation, i.LastSent.Format(time.RFC3339), oneLine(i.Latest)))
	}
	return out
}

// cmdStatus reports the queue, the dead letter and the open incidents, after sending what is due like
// every command. A due channel that cannot be built is a warning, and its entries are listed with
// the reason they are parked.
func cmdStatus(e *env, args []string) int {
	fs := newFlags("status")
	all := fs.Bool("all", false, "")
	project := fs.String("project", "", "")
	if err := parseFlags(fs, "status", statusUsage, args); err != nil {
		return e.fail(err)
	}
	proj, err := callerProject(*project)
	if err == nil && *all && flatLine(os.Getenv("YO_HUMAN_PROJECT")) != "" { // the same test as callerProject
		err = outOfProject("status lists only this project while YO_HUMAN_PROJECT names it")
	}
	if err != nil {
		return e.fail(err)
	}
	sc := scope{all: *all, project: proj}
	cfg, st, err := e.loadWithStore()
	if err != nil {
		return e.fail(err)
	}
	e.flushFirst(cfg, st)
	snap, err := takeSnapshot(cfg, st, now(), sc)
	if core.IsBusy(err) { // the incidents are read last: the rest of the snapshot stands
		e.warn(flatten(err))
		err = nil
	}
	if err != nil {
		return e.fail(err)
	}
	e.ok(snap.lines(), map[string]any{"queued": snap.Queued, "held": snap.Held, "dead": snap.Dead,
		"incidents": snap.Incidents, "inflight": snap.Inflight})
	return ExitOK
}

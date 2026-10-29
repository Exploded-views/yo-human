package core

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/problem"
	"github.com/Exploded-views/yo-human/internal/store"
)

// Deps is everything the pipeline needs from outside. Core makes no network calls of its own.
type Deps struct {
	Config   *config.Config
	Store    *store.Store
	Channels map[string]channel.Channel
	Caps     map[string]channel.Capabilities
	Now      func() time.Time
	NewID    func() string

	// ConfigPath is the explicit config file this run was given, if any. Entries record it, so
	// the flush a queued result asks for can name the same config.
	ConfigPath string

	// BacklogWorkers is how many backlog sends run at once; zero means eight.
	BacklogWorkers int
}

// Pipeline sends messages: enqueue first, fan out in parallel under the deadline, then retry.
type Pipeline struct {
	d       Deps
	workers int

	afterEnqueue func() // test seam: runs after the enqueue and before the first send
	afterClose   func() // test seam: runs in Resolve once the incident is marked closed, under the lock
	beforeLand   func() // test seam: runs in landed after a send succeeded, before the incident lock is taken
	beforeAdmit  func() // test seam: runs in admitDeferred once the waiting message is claimed, before its incident check
}

const defaultBacklogWorkers = 8

// New builds a pipeline over d.
func New(d Deps) *Pipeline {
	p := &Pipeline{d: d, workers: d.BacklogWorkers}
	if p.workers <= 0 {
		p.workers = defaultBacklogWorkers
	}
	return p
}

// payload is a queue entry's Data: the message plus the one send it is for, so a retry needs
// nothing from the config but the channel and its retry policy.
type payload struct {
	Message      channel.Message      `json:"message"`
	Generation   int                  `json:"generation"` // the incident generation the entry was written for; zero without a dedup key
	Enqueued     time.Time            `json:"enqueued"`   // the pipeline's clock at enqueue; the caller's Created is not trusted
	Instance     string               `json:"instance"`
	Fingerprint  string               `json:"fingerprint,omitempty"` // where the instance pointed when the entry was queued
	Config       string               `json:"config,omitempty"`      // the explicit config file it was queued under, if any
	Kind         string               `json:"kind,omitempty"`        // kindCancel for a queued cancellation, kindAdmit for a message waiting for its incident check; empty for a send
	ProviderID   string               `json:"provider_id,omitempty"` // the provider's id of the scheduled send a cancel is for
	Interruption channel.Interruption `json:"interruption"`
	Options      map[string]any       `json:"options,omitempty"`
	Plan         []PlannedSend        `json:"plan,omitempty"` // a kindAdmit entry's sends, each pinned to where it pointed
}

// Notify delivers m: every send its level calls for is written to the queue before any network
// call, then the due ones go out in parallel within the deadline, then older backlog uses the time
// left. The result describes m only. A non-nil error comes with whatever Result was reached.
func (p *Pipeline) Notify(ctx context.Context, m channel.Message) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.d.Config.Deadline) // the deadline bounds the whole call
	defer cancel()
	p = p.within(ctx)
	now := p.d.Now()
	if err := p.recoverStale(); err != nil {
		return Result{}, err
	}
	plan, err := Plan(p.d.Config, m.Level, now, p.d.Caps)
	if err != nil {
		return Result{}, err
	}
	p.stamp(&m, now)
	adm, err := p.admit(m, plan, now, time.Time{})
	if store.IsBusy(err) {
		return p.deferAdmission(m, plan)
	}
	if err != nil {
		return Result{ID: m.ID}, err
	}
	res, err := p.sendAdmitted(ctx, m, adm, now)
	err = busyAsWarnings(&res, err)
	p.withNext(&res, err, m.Level)
	return res, err
}

// busyAsWarnings turns a busy incident lock met once the call's own work is done (in the backlog it
// flushes after) into warnings: the result already says what happened to the message, and a busy
// lock must not make a delivered page exit 75. Both callers have set the result's status by then.
func busyAsWarnings(res *Result, err error) error {
	var keep []error
	for _, e := range splitErrs(err) {
		if store.IsBusy(e) {
			res.Warnings = append(res.Warnings, e)
		} else {
			keep = append(keep, e) // errors.Join drops a nil
		}
	}
	return errors.Join(keep...)
}

// within is the pipeline for one call: its waits for the incident lock end with ctx, the call's
// deadline (constitution VII).
func (p *Pipeline) within(ctx context.Context) *Pipeline {
	q := *p
	q.d.Store = p.d.Store.Within(ctx)
	return &q
}

// withNext gives a result that comes with a Queued problem (a busy lock, a queued cancel) the next
// step that problem asks for, when it has none.
func (p *Pipeline) withNext(res *Result, err error, level string) {
	if res.Next != nil || res.Status == "" {
		return
	}
	for _, e := range splitErrs(err) {
		if pr, ok := problem.As(e); ok && pr.Kind == problem.Queued {
			res.Next = p.next(level)
			return
		}
	}
}

// stamp gives m an id and a creation time if the caller left them out.
func (p *Pipeline) stamp(m *channel.Message, now time.Time) {
	if m.ID == "" {
		m.ID = p.d.NewID()
	}
	if m.Created.IsZero() {
		m.Created = now
	}
}

// sendAdmitted sends what the incident check let through and then whatever older backlog is due.
func (p *Pipeline) sendAdmitted(ctx context.Context, m channel.Message, adm admission, now time.Time) (Result, error) {
	b := &batch{p: p, now: now}
	if len(adm.plan)+len(adm.skipped) == 0 { // a suppressed repeat: recorded, nothing to send but whatever is due
		b.backlog(ctx, nil)
		return Result{Status: StatusSuppressed, ID: m.ID}, b.err()
	}
	if p.afterEnqueue != nil {
		p.afterEnqueue()
	}
	res := Result{ID: m.ID, Channels: append(b.fanOut(ctx, adm.entries, adm.plan), skippedResults(adm.skipped)...)}
	res.Status = p.statusOf(res.Channels)
	if res.Status == StatusQueued {
		res.Next = p.next(m.Level)
	}
	cfgErr := p.configProblem(res)
	res.Warnings = b.cancelAside(ctx, adm.cancel)
	skip := map[string]bool{}
	for _, e := range adm.entries {
		skip[e.pending.Key] = true
	}
	b.backlog(ctx, skip)
	return res, errors.Join(cfgErr, b.err())
}

// statusOf summarizes a message's sends. A send dropped because its incident was resolved
// mid-call makes the whole call a resolve outcome, not one to retry. A channel skipped because it
// could not be built will not deliver this message: it counts as dead.
func (p *Pipeline) statusOf(results []ChannelResult) Status {
	counted := make([]ChannelResult, len(results))
	for i, r := range results {
		if r.Status == ChannelCancelled {
			return StatusResolved
		}
		counted[i] = r
		if r.unbuilt {
			counted[i].Status = ChannelDead
		}
	}
	return Summarize(counted, p.humans())
}

// fanOut sends the message's entries in parallel and reports each in plan order. A send held by
// the queue is not attempted, and one the deadline kept from starting stays queued.
func (b *batch) fanOut(ctx context.Context, entries []entry, plan []PlannedSend) []ChannelResult {
	results := make([]ChannelResult, len(entries))
	var due []store.Pending
	var dueAt []int
	for i, e := range entries {
		results[i] = ChannelResult{Channel: e.instance, Status: b.waiting(e.pending)}
		if plan[i].HoldLocally {
			results[i].Status = ChannelHeld
			continue
		}
		due = append(due, e.pending)
		dueAt = append(dueAt, i)
	}
	for j, got := range b.drain(ctx, due, len(due), 0) {
		if got.Status != "" {
			results[dueAt[j]] = got
		}
	}
	return results
}

type entry struct {
	pending  store.Pending
	instance string
}

// incidentOf is the incident a queued message belongs to, which a resolve cancels. A resolve note
// belongs to none: it is sent because the incident closed.
func incidentOf(m channel.Message) string {
	if m.Resolved {
		return ""
	}
	return incidentID(m)
}

// enqueue writes one queue entry per planned send. Every entry is on disk before the first send.
func (p *Pipeline) enqueue(m channel.Message, gen int, plan []PlannedSend) ([]entry, error) {
	out := make([]entry, 0, len(plan))
	for _, s := range plan {
		fp := p.fingerprint(s.Instance)
		if s.Pinned {
			fp = s.Fingerprint
		}
		data, err := json.Marshal(payload{Message: m, Generation: gen, Enqueued: p.d.Now(), Instance: s.Instance,
			Fingerprint: fp, Config: p.d.ConfigPath, Interruption: s.Interruption, Options: s.Options})
		if err != nil {
			return nil, problem.Internalf("cannot encode the message for %s: %v", "a JSON-encodable message",
				"report this as a bug", s.Instance, err)
		}
		pd := store.Pending{Key: m.ID + "." + s.Instance, IncidentKey: incidentOf(m), Data: data, DeliverAt: s.DeliverAt}
		if err := p.d.Store.Enqueue(pd); err != nil {
			return out, err
		}
		out = append(out, entry{pending: pd, instance: s.Instance})
	}
	return out, nil
}

func (p *Pipeline) recoverStale() error {
	return RecoverStale(p.d.Config, p.d.Store)
}

// RecoverStale returns to the queue the entries in flight whose sender is gone. A command runs it
// before it builds the channels of what is due, so a recovered entry's channel is built too. It never
// waits for the incident lock: while another process holds it, recovery is left to a later call, and
// the call keeps its deadline for its own sends.
func RecoverStale(cfg *config.Config, st *store.Store) error {
	now, stop := context.WithCancel(context.Background())
	stop() // a lock that is not free at once is not waited for
	// File ages are real time, whatever clock the pipeline keeps.
	// No call lives longer than max(10 x deadline, 10 min): an entry in flight that long has an
	// owner pid that now belongs to another process.
	_, err := st.Within(now).Recover(time.Now(), 2*cfg.Deadline, max(10*cfg.Deadline, 10*time.Minute))
	if store.IsBusy(err) {
		return nil
	}
	return err
}

// humans maps each channel to whether it reaches a human.
func (p *Pipeline) humans() map[string]bool {
	out := make(map[string]bool, len(p.d.Caps))
	for name, c := range p.d.Caps {
		out[name] = c.ReachesHuman
	}
	return out
}

var defaultRetry = config.Retry{Every: 60 * time.Second, Times: 5}

func (p *Pipeline) retryFor(level string) config.Retry {
	if lv, ok := find(p.d.Config, level); ok {
		return lv.Retry
	}
	return defaultRetry
}

func (p *Pipeline) next(level string) *Next {
	r := p.retryFor(level)
	do := "yo-human flush"
	if p.d.ConfigPath != "" {
		do += " --config " + problem.ShellQuote(p.d.ConfigPath)
	}
	return &Next{Do: do, AfterSeconds: int(r.Every / time.Second), AttemptsLeft: sendsLeft(r, 1),
		Then: "tell the human in your session"}
}

// batch is one command's run of sends: it collects results and the first store errors from the
// goroutines that deliver.
type batch struct {
	p     *Pipeline
	now   time.Time
	mu    sync.Mutex
	errs  []error
	more  []ChannelResult // results of sends admitted from the queue during the batch
	warns []error         // warnings of those admissions
}

func (b *batch) fail(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.errs = append(b.errs, err)
}

// err is every failure of the batch. A Queued problem comes after the rest, so a failure that only
// the human can handle, or that retrying cannot fix, decides the exit code before "run the flush".
func (b *batch) err() error {
	var first, last []error
	for _, e := range b.errs {
		for _, one := range splitErrs(e) {
			if pr, ok := problem.As(one); ok && pr.Kind == problem.Queued {
				last = append(last, one)
			} else {
				first = append(first, one)
			}
		}
	}
	return errors.Join(append(first, last...)...)
}

// configProblem is the Config problem to return when nothing reached, or will reach, the human
// and a channel rejected its configuration: only the human can fix that, so it is exit 78.
func (p *Pipeline) configProblem(res Result) error {
	if res.Status != StatusFailed {
		return nil
	}
	humans := p.humans()
	for _, c := range res.Channels {
		if c.config && humans[c.Channel] {
			where := "the message may still be in the queue, so"
			if c.buried {
				where = "the message is in the dead letter,"
			}
			return problem.Configf("channel %s rejected its configuration: %s", "a channel whose settings its provider accepts",
				"tell the human to correct channel "+c.Channel+" in the config; "+where+" see \"yo-human status\"",
				c.Channel, c.Error)
		}
	}
	return nil
}

// waiting is how an entry that has not been sent stands: held while its delivery time is ahead
// (it would not go out sooner anyway), queued otherwise.
func (b *batch) waiting(p store.Pending) string {
	if p.DeliverAt.After(b.now) {
		return ChannelHeld
	}
	return ChannelQueued
}

package nudge_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
	"github.com/Exploded-views/yo-human/internal/hosted/nudge"
)

var t0 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// attempt is one call of Send: the message, when, and what the channel answered.
type attempt struct {
	m   notify.Message
	at  time.Time
	err error
}

// fakeChan is a channel that records every attempt and answers from a queue of results, then
// from def. Send blocks on gate when one is set.
type fakeChan struct {
	clk      *clock
	mu       sync.Mutex
	attempts []attempt
	queue    []error
	def      error
	gate     chan struct{}
	entered  chan struct{}
}

func (c *fakeChan) Name() string { return "fake" }
func (c *fakeChan) Levels() []notify.Level {
	return []notify.Level{notify.LevelPassive, notify.LevelActive}
}

func (c *fakeChan) Send(ctx context.Context, m notify.Message) error {
	if c.entered != nil {
		c.entered <- struct{}{}
	}
	if c.gate != nil {
		<-c.gate
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.def
	if len(c.queue) > 0 {
		err, c.queue = c.queue[0], c.queue[1:]
	}
	c.attempts = append(c.attempts, attempt{m, c.clk.now(), err})
	return err
}

func (c *fakeChan) script(errs ...error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = errs
}

func (c *fakeChan) setDefault(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.def = err
}

func (c *fakeChan) all() []attempt {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]attempt(nil), c.attempts...)
}

// delivered is the messages the channel accepted.
func (c *fakeChan) delivered() []notify.Message {
	var out []notify.Message
	for _, a := range c.all() {
		if a.err == nil {
			out = append(out, a.m)
		}
	}
	return out
}

// statusErr is what the Bark client returns for a status: the same classes.
func statusErr(code int) error {
	err := notify.WithStatus(fmt.Errorf("push refused with status %d", code), code)
	switch {
	case code == 401 || code == 403:
		return notify.Broken(err)
	case code == 429 || code == 408 || code >= 500:
		return err
	}
	return notify.Permanent(err)
}

var errNetwork = errors.New("connection refused")

type rig struct {
	t    *testing.T
	d    *db.DB
	path string
	b    *bus.Bus
	clk  *clock
	ch   *fakeChan
	w    *nudge.Worker
	cfg  nudge.Config
	logs *bytes.Buffer
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bus.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	clk := &clock{t: t0}
	r := &rig{t: t, d: d, path: path, clk: clk, ch: &fakeChan{clk: clk}, logs: &bytes.Buffer{}}
	r.cfg = nudge.Config{Window: 30 * time.Minute, MinSeverity: bus.SeverityNormal, Level: notify.LevelActive,
		Log: slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	r.b = bus.New(d, clk.now, bus.Limits{LinkHosts: []string{"github.com"}})
	r.w = nudge.NewWorker(r.b, d, r.ch, r.cfg, clk.now)
	return r
}

// restart is a new worker over the same database, as after a process restart.
func (r *rig) restart() {
	r.b = bus.New(r.d, r.clk.now, bus.Limits{LinkHosts: []string{"github.com"}})
	r.w = nudge.NewWorker(r.b, r.d, r.ch, r.cfg, r.clk.now)
}

func (r *rig) advance(d time.Duration) { r.clk.advance(d) }

func (r *rig) tick() {
	r.t.Helper()
	if err := r.w.Tick(context.Background()); err != nil {
		r.t.Fatalf("Tick: %v", err)
	}
}

// tickEvery advances by step and ticks, until total has passed.
func (r *rig) tickEvery(step, total time.Duration) {
	r.t.Helper()
	for elapsed := time.Duration(0); elapsed < total; elapsed += step {
		r.advance(step)
		r.tick()
	}
}

var (
	crewNode = bus.Principal{Kind: bus.PrincipalNode, Name: "ci-1", Swarm: "crew"}
	duet     = bus.Principal{Kind: bus.PrincipalDuet, Name: "duet"}
)

func question(handle string) bus.Draft {
	return bus.Draft{Kind: bus.KindQuestion, Severity: bus.SeverityNormal, To: "human", Handle: handle, Title: "Which port?", Summary: "A summary",
		Action: bus.ActionNone, Urgency: bus.UrgencyDigest}
}

func (r *rig) post(p bus.Principal, d bus.Draft) bus.Item {
	r.t.Helper()
	res, err := r.b.Post(context.Background(), p, d)
	if err != nil {
		r.t.Fatalf("Post: %v", err)
	}
	return res.Item
}

// duetThread is an item Duet started in the swarm crew: the thread nodes reply on.
func (r *rig) duetThread(handle string) bus.Item {
	d := question(handle)
	d.To = "crew"
	return r.post(duet, d)
}

func (r *rig) reply(p bus.Principal, thread, body string, sev bus.Severity, urg bus.Urgency) {
	r.t.Helper()
	if _, err := r.b.Reply(context.Background(), p, thread, body, sev, urg); err != nil {
		r.t.Fatalf("Reply: %v", err)
	}
}

func (r *rig) rows() []bus.OutboxRow {
	r.t.Helper()
	var rows []bus.OutboxRow
	if err := r.d.Tx(context.Background(), func(x bus.Tx) (err error) { rows, err = x.AllOutbox(); return }); err != nil {
		r.t.Fatal(err)
	}
	return rows
}

func (r *rig) row(id int64) bus.OutboxRow {
	r.t.Helper()
	for _, row := range r.rows() {
		if row.ID == id {
			return row
		}
	}
	r.t.Fatalf("no outbox row %d", id)
	return bus.OutboxRow{}
}

func (r *rig) do(fn func(x bus.Tx) error) {
	r.t.Helper()
	if err := r.d.Tx(context.Background(), fn); err != nil {
		r.t.Fatal(err)
	}
}

// pushed writes a ledger row: the item reached the owner's phone at `at`.
func (r *rig) pushed(itemID string, at time.Time) {
	r.t.Helper()
	r.do(func(x bus.Tx) error {
		return x.InsertPush(bus.Push{Source: bus.PushOutbox, SourceID: int64(at.Unix()), ItemID: itemID, Revision: 1, At: at})
	})
}

// queue adds an outbox row that is due now.
func (r *rig) queue(kind bus.OutboxKind, item bus.Item, title, body string) int64 {
	r.t.Helper()
	var id int64
	r.do(func(x bus.Tx) (err error) {
		id, err = x.InsertOutbox(bus.OutboxRow{Kind: kind, Swarm: item.Swarm, ItemID: item.ID, Revision: item.Revision, Level: "active",
			Title: title, Body: body, State: bus.OutboxPending, NextAt: r.clk.now(), Created: r.clk.now()})
		return err
	})
	return id
}

func (r *rig) delivery() *bus.DeliveryState {
	r.t.Helper()
	in, err := r.b.Inbox(context.Background(), duet, 0)
	if err != nil {
		r.t.Fatal(err)
	}
	return in.Delivery
}

func (r *rig) flag() bus.BrokenFlag {
	r.t.Helper()
	var f bus.BrokenFlag
	r.do(func(x bus.Tx) (err error) { f, err = x.BrokenFlag(); return })
	return f
}

// bodies is the text of what the channel accepted, in order.
func bodies(msgs []notify.Message) []string {
	out := []string{}
	for _, m := range msgs {
		out = append(out, m.Body)
	}
	return out
}

func wantBodies(t *testing.T, got []notify.Message, want ...string) {
	t.Helper()
	g := bodies(got)
	if len(g) != len(want) {
		t.Fatalf("delivered %q, want %q", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("delivered %q, want %q", g, want)
		}
	}
}

// quietItem is an item for the owner in the interrupt lane: no nudge counts it, so a test of
// the outbox sees only the outbox's messages.
func quietItem(handle string) bus.Draft {
	d := question(handle)
	d.Urgency = bus.UrgencyInterruptWhenGood
	return d
}

// duetSays is Duet's message in the thread of an owner item: it queues one outbox row.
func (r *rig) duetSays(it bus.Item, body string) {
	r.t.Helper()
	r.reply(duet, it.ID, body, bus.SeverityNormal, bus.UrgencyDigest)
}

// pushes is the messages the channel accepted that came from the outbox: they carry an id.
func (r *rig) pushes() []notify.Message {
	var out []notify.Message
	for _, m := range r.ch.delivered() {
		if m.ID != "" {
			out = append(out, m)
		}
	}
	return out
}

func (r *rig) ledger(itemID string) []bus.Push {
	r.t.Helper()
	var ps []bus.Push
	r.do(func(x bus.Tx) (err error) { ps, err = x.Pushes(itemID); return })
	return ps
}

func (r *rig) states() map[int64]bus.OutboxState {
	out := map[int64]bus.OutboxState{}
	for _, row := range r.rows() {
		out[row.ID] = row.State
	}
	return out
}

func idOf(id int64) string { return fmt.Sprintf("yh-%d", id) }

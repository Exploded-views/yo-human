package bus_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func (e *env) outbox(t *testing.T) []bus.OutboxRow {
	t.Helper()
	var rows []bus.OutboxRow
	if err := e.d.Tx(context.Background(), func(x bus.Tx) (err error) { rows, err = x.AllOutbox(); return }); err != nil {
		t.Fatal(err)
	}
	return rows
}

func duetNote(handle string, sev bus.Severity) bus.Draft {
	d := notice(handle)
	d.Severity, d.Summary, d.Title = sev, "Duet's summary", "Duet's title"
	return d
}

func TestDuetPostToHumanQueuesOneMessage(t *testing.T) {
	e := newEnv(t)
	for i, c := range []struct {
		sev  bus.Severity
		want string
	}{{bus.SeverityLow, "passive"}, {bus.SeverityNormal, "active"}, {bus.SeverityHigh, "active"}, {bus.SeverityCritical, "active"}} {
		r := e.post(t, duet, duetNote("duet-"+string(c.sev), c.sev))
		rows := e.outbox(t)
		if len(rows) != i+1 {
			t.Fatalf("%s: %d rows, want %d", c.sev, len(rows), i+1)
		}
		got := rows[i]
		want := bus.OutboxRow{ID: got.ID, Kind: bus.OutboxDuetPost, Swarm: "duet", ItemID: r.Item.ID, Revision: 1, Level: c.want,
			Title: "Duet's title", Body: "Duet's summary", State: bus.OutboxPending, NextAt: got.Created, Created: got.Created}
		if got != want || got.Created.IsZero() || got.Created.After(e.now()) {
			t.Errorf("%s: row = %+v, want %+v", c.sev, got, want)
		}
	}
}

func TestDuetPostCarriesAtMostABoundedBody(t *testing.T) {
	e := newEnv(t)
	d := duetNote("long", bus.SeverityNormal)
	d.Summary = strings.Repeat("é", 900)
	e.post(t, duet, d)
	rows := e.outbox(t)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	row := rows[0]
	if n := len([]rune(row.Body)); n == 0 || n > 300 || !strings.HasPrefix(d.Summary, row.Body) {
		t.Errorf("body has %d runes, want a prefix of the summary of at most 300", n)
	}
}

func TestNodePageQueuesAPageRow(t *testing.T) {
	e := newEnv(t)
	d := notice("down")
	d.Urgency, d.Severity = bus.UrgencyPage, bus.SeverityHigh
	r := e.post(t, node("ci-1", "crew"), d)
	rows := e.outbox(t)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	got := rows[0]
	want := bus.OutboxRow{ID: got.ID, Kind: bus.OutboxPage, Swarm: "crew", ItemID: r.Item.ID, Revision: 1, Level: "active",
		Title: "yo-human", Body: "1 page for Claude", State: bus.OutboxPending, NextAt: got.Created, Created: got.Created}
	if got != want {
		t.Errorf("row = %+v, want %+v (counts only: no item text)", got, want)
	}
}

func TestOtherLanesAndAddresseesQueueNothing(t *testing.T) {
	e := newEnv(t)
	e.post(t, node("ci-1", "crew"), notice("digest"))
	g := notice("good")
	g.Urgency = bus.UrgencyInterruptWhenGood
	e.post(t, node("ci-1", "crew"), g)
	to := notice("to-crew")
	to.To = "crew"
	e.post(t, duet, to)
	if rows := e.outbox(t); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
}

func TestRepostQueuesOnlyWhenTheItemChanged(t *testing.T) {
	e := newEnv(t)
	d := duetNote("h1", bus.SeverityNormal)
	first := e.post(t, duet, d)
	if again := e.post(t, duet, d); again.Outcome != bus.OutcomeUnchanged {
		t.Fatalf("outcome = %s", again.Outcome)
	}
	if n := len(e.outbox(t)); n != 1 {
		t.Fatalf("%d rows after an unchanged repost, want 1", n)
	}
	d.Summary = "A new summary"
	if r := e.post(t, duet, d); r.Outcome != bus.OutcomeRevised || r.Item.ID != first.Item.ID {
		t.Fatalf("revise = %+v", r)
	}
	rows := e.outbox(t)
	if len(rows) != 2 || rows[1].Revision != 2 || rows[1].Body != "A new summary" || rows[1].ItemID != first.Item.ID {
		t.Errorf("rows = %+v, want a second for revision 2", rows)
	}
}

// failingOutbox is a Store whose InsertOutbox fails.
type failingOutbox struct{ bus.Store }

type failingOutboxTx struct{ bus.Tx }

var errOutbox = errors.New("outbox is on fire /secret/path")

func (f failingOutbox) Tx(ctx context.Context, fn func(bus.Tx) error) error {
	return f.Store.Tx(ctx, func(x bus.Tx) error { return fn(failingOutboxTx{x}) })
}

func (failingOutboxTx) InsertOutbox(bus.OutboxRow) (int64, error) { return 0, errOutbox }

func TestOutboxRowIsInTheSameTransactionAsThePost(t *testing.T) {
	e := newEnv(t)
	b := bus.New(failingOutbox{e.d}, e.now, e.l)
	_, err := b.Post(context.Background(), duet, duetNote("h1", bus.SeverityNormal))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v, want a refusal that names no path", err)
	}
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events stored by a post whose outbox row failed", n)
	}
	if _, found := e.stored(t, "anything"); found {
		t.Error("an item was stored")
	}
}

func TestDuetReplyOnAnOwnerItemQueuesAMessage(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	body := strings.Repeat("x", 500)
	if _, err := e.b.Reply(context.Background(), duet, it.ID, body, bus.SeverityHigh, bus.UrgencyDigest); err != nil {
		t.Fatal(err)
	}
	rows := e.outbox(t)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1", len(rows))
	}
	r := rows[0]
	want := bus.OutboxRow{ID: r.ID, Kind: bus.OutboxDuetPost, Swarm: "crew", ItemID: it.ID, Revision: 1, Level: "active", Title: "yo-human",
		Body: body[:300], State: bus.OutboxPending, NextAt: r.Created, Created: r.Created}
	if r != want {
		t.Errorf("row = %+v, want %+v", r, want)
	}
}

func TestReplyQueuesNothingOtherwise(t *testing.T) {
	e := newEnv(t)
	own := notice("to-crew")
	own.To = "crew"
	duetsItem := e.post(t, duet, own).Item // Duet's thread to the crew: the owner is not its addressee
	ownerItem := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	ctx := context.Background()
	steps := []struct {
		p   bus.Principal
		id  string
		urg bus.Urgency
	}{{node("ci-2", "crew"), duetsItem.ID, bus.UrgencyPage}, {duet, duetsItem.ID, bus.UrgencyDigest},
		{node("ci-2", "crew"), ownerItem.ID, bus.UrgencyDigest}, {node("ci-2", "crew"), ownerItem.ID, bus.UrgencyInterruptWhenGood}}
	for _, s := range steps {
		if _, err := e.b.Reply(ctx, s.p, s.id, "hi", bus.SeverityNormal, s.urg); err != nil {
			t.Fatal(err)
		}
	}
	if rows := e.outbox(t); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
}

func TestNodePageReplyQueuesAPageRow(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	if _, err := e.b.Reply(context.Background(), node("ci-2", "crew"), it.ID, "now", bus.SeverityNormal, bus.UrgencyPage); err != nil {
		t.Fatal(err)
	}
	rows := e.outbox(t)
	if len(rows) != 1 || rows[0].Kind != bus.OutboxPage || rows[0].ItemID != it.ID || rows[0].Swarm != "crew" || rows[0].Body != "1 page for Claude" {
		t.Errorf("rows = %+v, want one page row for the thread", rows)
	}
}

func TestOnCommitIsCalledAfterACommittedWrite(t *testing.T) {
	e := newEnv(t)
	calls := 0
	l := e.l
	l.OnCommit = func() { calls++ }
	b := bus.New(e.d, e.now, l)
	ctx := context.Background()
	d := notice("h1")
	if _, err := b.Post(ctx, node("ci-1", "crew"), d); err != nil || calls != 1 {
		t.Fatalf("after a post: calls=%d err=%v, want 1", calls, err)
	}
	if _, err := b.Post(ctx, node("ci-1", "crew"), d); err != nil || calls != 1 {
		t.Fatalf("after an unchanged repost: calls=%d err=%v, want still 1", calls, err)
	}
	bad := notice("h2")
	bad.Urgency = ""
	if _, err := b.Post(ctx, node("ci-1", "crew"), bad); err == nil || calls != 1 {
		t.Fatalf("after a refused post: calls=%d err=%v, want still 1", calls, err)
	}
}

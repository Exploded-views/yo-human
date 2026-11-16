package db_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

// The convention Task 7 reads events by: an event's Created equals the Recorded of the row it
// records (a decision, or a relay). The event row has no revision or row id of its own, so the
// row is found by (item, recorded time), and the revision is the row's.
func TestSupersededEventIdentifiesRows(t *testing.T) {
	d, itemID, agent, human := supersedeScenario(t)
	ctx := context.Background()
	var evs []bus.Event
	_ = d.Tx(ctx, func(x bus.Tx) (err error) { evs, err = x.EventsSince("", 0, db.MaxEvents); return err })
	sup := eventsOfType(evs, bus.EventAnswerSuperseded)
	if len(sup) != 1 || !sup[0].Created.Equal(human.Recorded) {
		t.Fatalf("superseded events = %+v, want one created at the human row's Recorded %v", sup, human.Recorded)
	}
	rows, err := d.RowsRecordedAt(ctx, itemID, sup[0].Created)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows at the event's Created = %+v, %v", rows, err)
	}
	pair, err := d.RowsOfRevision(ctx, itemID, rows[0].Revision)
	got := []any{err, rows[0].ActorKind, rows[0].Revision, rows[0].Text, len(pair)}
	want := []any{nil, bus.ActorHuman, 1, "use 9090", 2}
	if !reflect.DeepEqual(got, want) || pair[0].Text != agent.Text || pair[1].Text != human.Text {
		t.Errorf("event resolves to %+v and revision 1 rows %+v, want %+v", got, pair, want)
	}
}

// supersedeScenario: Duet answers revision 1, the desk supersedes it, the item is revised, and
// the desk answers revision 2.
func supersedeScenario(t *testing.T) (d *db.DB, itemID string, agent, human bus.Record) {
	t.Helper()
	d, _ = openTemp(t)
	n := 0
	now := func() time.Time { n++; return t0.Add(time.Duration(n) * time.Second) }
	b := bus.New(d, now, bus.Limits{})
	ctx := context.Background()
	crew := bus.Principal{Kind: bus.PrincipalNode, Name: "ci-1", Swarm: "crew"}
	duet := bus.Principal{Kind: bus.PrincipalDuet, Name: "duet"}
	deskP := bus.Principal{Kind: bus.PrincipalDesk, Name: "desk"}
	q := bus.Draft{Kind: bus.KindQuestion, Severity: bus.SeverityNormal, To: "human", Handle: "q", Title: "Which port?", Action: bus.ActionNone, Urgency: bus.UrgencyDigest}
	it, err := b.Post(ctx, crew, q)
	if err != nil {
		t.Fatal(err)
	}
	must := func(r bus.Record, err error) bus.Record {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	answer := func(p bus.Principal, rev int, text string) bus.Record {
		return must(b.RecordAnswer(ctx, p, bus.AnswerInput{ItemID: it.Item.ID, Revision: rev, Text: text, HumanTS: t0}))
	}
	agent = answer(duet, 1, "use 8080")
	human = answer(deskP, 1, "use 9090")
	q.Title = "Which port now?"
	if r, err := b.Post(ctx, crew, q); err != nil || r.Item.Revision != 2 {
		t.Fatalf("revise = %+v, %v", r, err)
	}
	answer(deskP, 2, "use 7070")
	return d, it.Item.ID, agent, human
}

func eventsOfType(evs []bus.Event, ty bus.EventType) (out []bus.Event) {
	for _, ev := range evs {
		if ev.Type == ty {
			out = append(out, ev)
		}
	}
	return out
}

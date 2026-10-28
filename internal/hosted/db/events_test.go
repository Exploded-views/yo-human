package db_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

func ev(swarm string, typ bus.EventType) bus.Event {
	return bus.Event{Swarm: swarm, ItemID: "itm_1", Type: typ, Created: t0}
}

func seqsOf(es []bus.Event) []int64 {
	out := make([]int64, len(es))
	for i, e := range es {
		out[i] = e.Seq
	}
	return out
}

func eventsSince(t *testing.T, d *db.DB, swarm string, since int64, limit int) []bus.Event {
	t.Helper()
	var got []bus.Event
	tx(t, d, func(x bus.Tx) (err error) { got, err = x.EventsSince(swarm, since, limit); return })
	return got
}

func appendAll(t *testing.T, d *db.DB, es ...bus.Event) []int64 {
	t.Helper()
	var seqs []int64
	tx(t, d, func(x bus.Tx) error {
		for _, e := range es {
			s, err := x.AppendEvent(e)
			if err != nil {
				return err
			}
			seqs = append(seqs, s)
		}
		return nil
	})
	return seqs
}

func TestEventsSinceIsOrderedAndScoped(t *testing.T) {
	d, _ := openTemp(t)
	seqs := appendAll(t, d, ev("crew", bus.EventItemPosted), ev("client", bus.EventItemPosted),
		ev("crew", bus.EventItemRevised), ev("", bus.EventDeskSigninStarted))
	if !slices.IsSorted(seqs) || len(slices.Compact(slices.Clone(seqs))) != 4 {
		t.Fatalf("seqs are not strictly increasing: %v", seqs)
	}
	crew := eventsSince(t, d, "crew", 0, 100)
	if !slices.Equal(seqsOf(crew), []int64{seqs[0], seqs[2]}) {
		t.Fatalf("crew events: %+v", crew)
	}
	for _, e := range crew {
		if e.Swarm != "crew" {
			t.Fatalf("crew's events include swarm %q", e.Swarm)
		}
	}
	if got := seqsOf(eventsSince(t, d, "", 0, 100)); !slices.Equal(got, seqs) {
		t.Fatalf("all events: %v, want %v", got, seqs)
	}
	if got := seqsOf(eventsSince(t, d, "crew", seqs[0], 100)); !slices.Equal(got, []int64{seqs[2]}) {
		t.Fatalf("crew after the first: %v", got)
	}
	if got := seqsOf(eventsSince(t, d, "", 0, 2)); !slices.Equal(got, seqs[:2]) {
		t.Fatalf("limit 2: %v", got)
	}
}

func TestEventRoundTrip(t *testing.T) {
	d, _ := openTemp(t)
	want := bus.Event{
		Swarm: "", ItemID: "", Type: bus.EventDeskSigninStarted, Created: t0,
		Starter: "starter-id", Phrase: "two words", UserCode: "ABCD-1234", Expires: t0.Add(10 * time.Minute),
	}
	var got []bus.Event
	tx(t, d, func(x bus.Tx) error {
		s, err := x.AppendEvent(want)
		if err != nil {
			return err
		}
		want.Seq = s
		got, err = x.EventsSince("", 0, 10)
		return err
	})
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestMessagesRoundTripInOrder(t *testing.T) {
	d, _ := openTemp(t)
	insertItem(t, d, item("itm_1", "crew", "first"))
	m1 := bus.Message{ID: "msg_1", ThreadID: "itm_1", From: "ci-1", FromKind: bus.PrincipalNode, Body: "hello", Severity: bus.SeverityLow, Created: t0}
	m2 := bus.Message{ID: "msg_2", ThreadID: "itm_1", From: "owner", FromKind: bus.PrincipalDuet, Body: "hi", Severity: bus.SeverityNormal, Created: t0.Add(time.Second)}
	other := bus.Message{ID: "msg_3", ThreadID: "itm_2", From: "x", FromKind: bus.PrincipalNode, Body: "z", Severity: bus.SeverityLow, Created: t0}
	var got []bus.Message
	tx(t, d, func(x bus.Tx) (err error) {
		for _, m := range []bus.Message{m2, m1, other} {
			if err = x.InsertMessage(m); err != nil {
				return err
			}
		}
		got, err = x.Messages("itm_1")
		return err
	})
	if !reflect.DeepEqual(got, []bus.Message{m1, m2}) {
		t.Fatalf("got %+v", got)
	}
}

func TestStatusPutReplacesAndScopes(t *testing.T) {
	d, _ := openTemp(t)
	first := bus.StatusDoc{Swarm: "crew", Doc: json.RawMessage(`{"v":1}`), Updated: t0}
	second := bus.StatusDoc{Swarm: "crew", Doc: json.RawMessage(`{"v":2}`), Updated: t0.Add(time.Second)}
	other := bus.StatusDoc{Swarm: "client", Doc: json.RawMessage(`{"v":3}`), Updated: t0}
	var crew, all []bus.StatusDoc
	tx(t, d, func(x bus.Tx) (err error) {
		for _, s := range []bus.StatusDoc{first, other, second} {
			if err = x.PutStatus(s); err != nil {
				return err
			}
		}
		if crew, err = x.Statuses("crew"); err != nil {
			return err
		}
		all, err = x.Statuses("")
		return err
	})
	if len(crew) != 1 || !reflect.DeepEqual(crew[0], second) {
		t.Fatalf("crew statuses: %+v", crew)
	}
	if len(all) != 2 || all[0].Swarm != "client" {
		t.Fatalf("all statuses: %+v", all)
	}
}

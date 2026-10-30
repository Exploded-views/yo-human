package bus_test

import (
	"context"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// A missing or unknown urgency is refused, never defaulted: only the poster knows the lane.
func TestPostWithoutUrgencyRefused(t *testing.T) {
	e := newEnv(t)
	for _, u := range []bus.Urgency{"", "soon", "Page", "interrupt_when_good", " digest"} {
		for _, p := range []bus.Principal{node("ci-1", "crew"), duet} {
			d := notice("h1")
			d.Urgency = u
			if p.Kind == bus.PrincipalDuet {
				d.To = "crew"
			}
			_, err := e.b.Post(context.Background(), p, d)
			if be := errWithCode(t, err, bus.CodeInvalid); be.What != "urgency" {
				t.Errorf("%s posting urgency %q: What = %q, want urgency", p.Kind, u, be.What)
			}
		}
	}
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after refused posts", n)
	}
}

func TestReplyWithoutUrgencyRefused(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, node("ci-1", "crew"), notice("h1")).Item
	for _, u := range []bus.Urgency{"", "soon", "Page"} {
		for _, p := range []bus.Principal{node("ci-2", "crew"), duet} {
			_, err := e.b.Reply(context.Background(), p, it.ID, "hi", bus.SeverityNormal, u)
			if be := errWithCode(t, err, bus.CodeInvalid); be.What != "urgency" {
				t.Errorf("%s replying urgency %q: What = %q, want urgency", p.Kind, u, be.What)
			}
		}
	}
	if len(e.messages(t, it.ID)) != 0 {
		t.Error("a refused reply stored a message")
	}
}

// Duet is the one deciding when to interrupt, so it takes page or digest only.
func TestDuetCannotPostInterruptWhenGood(t *testing.T) {
	e := newEnv(t)
	d := notice("h1")
	d.To, d.Urgency = "crew", bus.UrgencyInterruptWhenGood
	_, err := e.b.Post(context.Background(), duet, d)
	if be := errWithCode(t, err, bus.CodeInvalid); be.What != "urgency" {
		t.Errorf("post: What = %q", be.What)
	}
	it := e.post(t, node("ci-1", "crew"), notice("h2")).Item
	_, err = e.b.Reply(context.Background(), duet, it.ID, "hi", bus.SeverityNormal, bus.UrgencyInterruptWhenGood)
	if be := errWithCode(t, err, bus.CodeInvalid); be.What != "urgency" {
		t.Errorf("reply: What = %q", be.What)
	}
	for i, u := range []bus.Urgency{bus.UrgencyPage, bus.UrgencyDigest} {
		d.Urgency, d.Handle = u, "ok-"+string(u)
		if _, err := e.b.Post(context.Background(), duet, d); err != nil {
			t.Errorf("duet post %s: %v", u, err)
		}
		if _, err := e.b.Reply(context.Background(), duet, it.ID, "hi", bus.SeverityNormal, u); err != nil {
			t.Errorf("duet reply %s (%d): %v", u, i, err)
		}
	}
	// A node may queue for Duet's choice.
	n := node("ci-1", "crew")
	q := notice("h3")
	q.Urgency = bus.UrgencyInterruptWhenGood
	e.post(t, n, q)
	if _, err := e.b.Reply(context.Background(), n, it.ID, "hi", bus.SeverityNormal, bus.UrgencyInterruptWhenGood); err != nil {
		t.Errorf("node reply: %v", err)
	}
}

func TestUrgencyRoundTripsThroughTheStore(t *testing.T) {
	e := newEnv(t)
	p := node("ci-1", "crew")
	for i, u := range []bus.Urgency{bus.UrgencyPage, bus.UrgencyInterruptWhenGood, bus.UrgencyDigest} {
		d := notice("h" + string(rune('a'+i)))
		d.Urgency = u
		it := e.post(t, p, d).Item
		if got, _ := e.stored(t, it.ID); got.Urgency != u || it.Urgency != u {
			t.Errorf("stored urgency = %q (result %q), want %q", got.Urgency, it.Urgency, u)
		}
		m, err := e.b.Reply(context.Background(), p, it.ID, "hi", bus.SeverityNormal, u)
		if err != nil {
			t.Fatal(err)
		}
		if got := e.messages(t, it.ID); len(got) != 1 || got[0].Urgency != u || m.Urgency != u {
			t.Errorf("message urgency = %+v (result %q), want %q", got, m.Urgency, u)
		}
	}
}

// The urgency is content: changing only it is a revision, and the stored lane follows.
func TestUrgencyChangeIsARevision(t *testing.T) {
	e := newEnv(t)
	p := node("ci-1", "crew")
	d := notice("h1")
	d.Urgency = bus.UrgencyDigest
	it := e.post(t, p, d).Item
	d.Urgency = bus.UrgencyPage
	r := e.post(t, p, d)
	if r.Outcome != bus.OutcomeRevised || r.Item.Revision != 2 || r.Item.ContentHash == it.ContentHash {
		t.Fatalf("urgency-only repost = %s revision %d", r.Outcome, r.Item.Revision)
	}
	if got, _ := e.stored(t, it.ID); got.Urgency != bus.UrgencyPage {
		t.Errorf("stored urgency = %q after the revision", got.Urgency)
	}
}

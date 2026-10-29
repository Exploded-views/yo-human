package bus_test

import (
	"context"
	"slices"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

func TestValidSwarmReservesDuetAndHuman(t *testing.T) {
	for s, want := range map[string]bool{"crew": true, "client": true, "a": true, "duet": false, "human": false,
		"": false, "Crew": false, "-x": false, "duet2": true} {
		if got := bus.ValidSwarm(s); got != want {
			t.Errorf("ValidSwarm(%q) = %v, want %v", s, got, want)
		}
	}
}

// Duet's owner-addressed items live in swarm duet. A node token claiming that swarm must reach
// none of them, and Duet can't address a crew named duet or human.
func TestNodeInSwarmDuetReachesNothing(t *testing.T) {
	e := newEnv(t)
	it := e.post(t, duet, notice("for-owner")).Item
	if it.Swarm != "duet" {
		t.Fatalf("swarm = %q", it.Swarm)
	}
	for _, sw := range []string{"duet", "human"} {
		n := node("n", sw)
		_, err := e.b.Reply(context.Background(), n, it.ID, "hi", bus.SeverityNormal, bus.UrgencyDigest)
		wantCode(t, err, bus.CodeForbidden)
		_, err = e.b.Post(context.Background(), n, notice("x"))
		wantCode(t, err, bus.CodeForbidden)
		if bus.New(nil, nil, bus.Limits{}).Visible(n, it) {
			t.Errorf("a node in swarm %q sees Duet's item", sw)
		}
	}
	if len(e.messages(t, it.ID)) != 0 || len(e.events(t)) != 1 {
		t.Error("a refused call stored something")
	}
}

func TestDuetCannotAddressReservedSwarms(t *testing.T) {
	e := newEnv(t)
	d := notice("h1")
	d.To = "duet"
	_, err := e.b.Post(context.Background(), duet, d)
	if be := errWithCode(t, err, bus.CodeInvalid); be.What != "to" {
		t.Errorf("What = %q, want to", be.What)
	}
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after a refused post", n)
	}
}

// The void check reads the revision being replaced, not revision 1 and not a fixed one.
func TestVoidReadsTheCurrentRevision(t *testing.T) {
	e := newEnv(t)
	p := node("ci-1", "crew")
	repost := func(title string) {
		d := mergeApproval("m7")
		d.Title = title
		e.post(t, p, d)
	}
	id := e.post(t, p, mergeApproval("m7")).Item.ID
	approveRevision(t, e, id, 1)
	repost("rev 2") // revision 1 had an approval: void
	repost("rev 3") // revision 2 had none: no void
	approveRevision(t, e, id, 3)
	repost("rev 4") // revision 3 had one: void
	want := []bus.EventType{bus.EventItemPosted,
		bus.EventItemRevised, bus.EventApprovalVoided,
		bus.EventItemRevised,
		bus.EventItemRevised, bus.EventApprovalVoided}
	if got := types(e.events(t)); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

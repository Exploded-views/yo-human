package bus_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// A walled item carries pinned_commit and pinned_tree only on the actions that use them: a merge
// and a release (lead ruling, 2026-10-30). Anything else with either field set is invalid, naming
// the field, on a post, a revision and Duet's post into the swarm. An unwalled swarm is unchanged.
func TestWalledPinsOnlyOnMergeAndRelease(t *testing.T) {
	e := newEnvWalled(t, "client")
	sha := strings.Repeat("0a", 20)
	for _, c := range []struct {
		name, field string
		edit        func(*bus.Draft)
	}{
		{"none with a pinned commit", "pinned_commit", func(d *bus.Draft) { d.PinnedCommit = sha }},
		{"none with a pinned tree", "pinned_tree", func(d *bus.Draft) { d.PinnedTree = sha }},
		{"none with both", "pinned_commit", func(d *bus.Draft) { d.PinnedCommit, d.PinnedTree = sha, sha }},
		{"other with a pinned commit", "pinned_commit", func(d *bus.Draft) { d.Action, d.PinnedCommit = bus.ActionOther, sha }},
		{"deploy with a pinned tree", "pinned_tree", func(d *bus.Draft) { d.Action, d.PinnedTree = bus.ActionDeploy, sha }},
		{"public-sync with a pinned tree", "pinned_tree", func(d *bus.Draft) { d.Action, d.PinnedTree = bus.ActionPublicSync, sha }},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := typed("12")
			c.edit(&d)
			_, err := e.b.Post(context.Background(), walledNode, d)
			if be := errWithCode(t, err, bus.CodeInvalid); be.What != c.field {
				t.Errorf("What = %q, want %q", be.What, c.field)
			}
		})
	}
	// A revision is held to it too.
	e.post(t, walledNode, typed("12"))
	rev := typed("12")
	rev.Title = "ob-12 ack"
	rev.PinnedTree = sha
	_, err := e.b.Post(context.Background(), walledNode, rev)
	if be := errWithCode(t, err, bus.CodeInvalid); be.What != "pinned_tree" {
		t.Errorf("revision: What = %q", be.What)
	}
	// Duet's post into the swarm carries no pins either.
	into := bus.Draft{Kind: bus.KindNotice, Severity: bus.SeverityNormal, To: "client", Title: "go sl:12", Action: bus.ActionNone,
		Urgency: bus.UrgencyDigest, PinnedCommit: sha}
	_, err = e.b.Post(context.Background(), duet, into)
	if be := errWithCode(t, err, bus.CodeInvalid); be.What != "pinned_commit" {
		t.Errorf("Duet into the swarm: What = %q", be.What)
	}
	// A merge and a release may carry both.
	m := walledMerge()
	m.PinnedTree = sha
	e.post(t, walledNode, m)
	r := walledRelease()
	r.PinnedTree = sha
	e.post(t, walledNode, r)
	// Unwalled: unchanged, an action none item may carry them.
	u := notice("u-1")
	u.PinnedCommit, u.PinnedTree = sha, sha
	e.post(t, node("ci-1", "crew"), u)
}

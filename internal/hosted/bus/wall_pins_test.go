package bus_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// A walled item carries pinned_commit and pinned_tree only where its action requires them, which
// is what Validate's required map says: pinned_commit for a merge, release, deploy and public-sync,
// pinned_tree for public-sync (lead ruling 2026-10-30: pins only where the action requires them).
// Any other pin is invalid, naming the field, on a post, a revision and Duet's post into the swarm.
// An unwalled swarm is unchanged.
func TestWalledPinsOnlyWhereTheActionRequiresThem(t *testing.T) {
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
		{"other with a pinned tree", "pinned_tree", func(d *bus.Draft) { d.Action, d.PinnedTree = bus.ActionOther, sha }},
		{"merge with a pinned tree", "pinned_tree", func(d *bus.Draft) { *d = walledMerge(); d.PinnedTree = sha }},
		{"release with a pinned tree", "pinned_tree", func(d *bus.Draft) { *d = walledRelease(); d.PinnedTree = sha }},
		{"deploy with a pinned tree", "pinned_tree", func(d *bus.Draft) { *d = walledDeploy(); d.PinnedTree = sha }},
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
	// Where the action requires them they are accepted.
	e.post(t, walledNode, walledMerge())      // pinned_commit
	e.post(t, walledNode, walledRelease())    // pinned_commit
	e.post(t, walledNode, walledDeploy())     // pinned_commit
	e.post(t, walledNode, walledPublicSync()) // both
	// Unwalled: unchanged, an action none item may carry them.
	u := notice("u-1")
	u.PinnedCommit, u.PinnedTree = sha, sha
	e.post(t, node("ci-1", "crew"), u)
}

func walledDeploy() bus.Draft {
	return bus.Draft{
		Kind: bus.KindApproval, Severity: bus.SeverityHigh, To: "human", Handle: "ob-14", Title: "ob-14 deploy-ready",
		Action: bus.ActionDeploy, Repo: "cn:alpha", Target: "prod", PinnedCommit: strings.Repeat("0a", 20), Urgency: bus.UrgencyPage,
	}
}

func walledPublicSync() bus.Draft {
	return bus.Draft{
		Kind: bus.KindApproval, Severity: bus.SeverityHigh, To: "human", Handle: "ob-15", Title: "ob-15 deploy-ready",
		Action: bus.ActionPublicSync, Repo: "cn:alpha", Ref: "main", PinnedCommit: strings.Repeat("0a", 20), PinnedTree: strings.Repeat("0b", 20),
		Urgency: bus.UrgencyPage,
	}
}

package bus_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
)

// The walled swarm in these tests is "client"; "crew" is not walled.
var walledNode = node("w-1", "client")

func typed(n string) bus.Draft {
	return bus.Draft{Kind: bus.KindNotice, Severity: bus.SeverityNormal, To: "human", Handle: "ob-" + n,
		Title: "ob-" + n + " needs:owner", Action: bus.ActionNone, Urgency: bus.UrgencyDigest}
}

func walledRelease() bus.Draft {
	return bus.Draft{
		Kind: bus.KindApproval, Severity: bus.SeverityHigh, To: "human", Handle: "ob-12", Title: "ob-12 deploy-ready",
		Action: bus.ActionRelease, Target: "prod", Repo: "cn:alpha", Ref: "v1.2.3", PinnedCommit: strings.Repeat("0a", 20), Urgency: bus.UrgencyPage,
	}
}

// walledMerge is a walled merge approval for a pull request: a PR id as the ref, a branch as the
// base (round 3: the lead's ruling of 2026-10-29).
func walledMerge() bus.Draft {
	return bus.Draft{
		Kind: bus.KindApproval, Severity: bus.SeverityHigh, To: "human", Handle: "ob-13", Title: "ob-13 deploy-ready",
		Action: bus.ActionMerge, Repo: "cn:alpha", Ref: "pr:42", BaseRef: "main", Target: "cn:alpha#42",
		PinnedCommit: strings.Repeat("0a", 20), Urgency: bus.UrgencyPage,
	}
}

func TestWalledPostRefusesFreeText(t *testing.T) {
	e := newEnvWalled(t, "client")
	for _, c := range []struct {
		name, field string
		edit        func(*bus.Draft)
	}{
		{"prose title", "title", func(d *bus.Draft) { d.Title = "The deploy to the client's server is stuck" }},
		{"typed title with trailing words", "title", func(d *bus.Draft) { d.Title = "ob-12 blocked on the database" }},
		{"unknown state", "title", func(d *bus.Draft) { d.Title = "ob-12 stuck" }},
		{"title for another number", "title", func(d *bus.Draft) { d.Title = "ob-12x needs:owner" }},
		{"half a digest suffix", "title", func(d *bus.Draft) { d.Title = "ob-12 done sha256:abc bytes:3" }},
		{"prose handle", "handle", func(d *bus.Draft) { d.Handle = "client-deploy" }},
		{"empty handle", "handle", func(d *bus.Draft) { d.Handle = "" }},
		{"summary", "summary", func(d *bus.Draft) { d.Summary = "details" }},
		{"detail", "detail", func(d *bus.Draft) { d.Detail = "details" }},
		{"required action", "required_action", func(d *bus.Draft) { d.RequiredAction = "please look" }},
		{"link", "link", func(d *bus.Draft) { d.Link = "https://github.com/o/r/pull/1" }},
		{"prose target", "target", func(d *bus.Draft) { d.Target = "the client's site" }},
	} {
		d := typed("12")
		c.edit(&d)
		_, err := e.b.Post(context.Background(), walledNode, d)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != c.field {
			t.Errorf("%s: What = %q, want %q", c.name, be.What, c.field)
		}
	}
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after refused posts", n)
	}
	digest := strings.Repeat("ab", 32)
	for _, title := range []string{"ob-12 needs:owner", "ob-12 needs:none", "ob-12 ack", "ob-12 done", "ob-12 blocked", "ob-12 ci-red",
		"ob-12 deploy-ready", "ob-12 prod-down", "ob-12 done sha256:" + digest + " bytes:2048"} {
		d := typed("12")
		d.Title = title
		d.Handle = "ob-12"
		e.post(t, walledNode, d)
	}
}

func TestWalledRepostWithProseRefused(t *testing.T) {
	e := newEnvWalled(t, "client")
	e.post(t, walledNode, typed("12"))
	d := typed("12")
	d.Summary = "now with words"
	_, err := e.b.Post(context.Background(), walledNode, d)
	wantCode(t, err, bus.CodeInvalid)
	d = typed("12")
	d.Title = "ob-12 done"
	if r := e.post(t, walledNode, d); r.Outcome != bus.OutcomeRevised {
		t.Errorf("typed revision = %s", r.Outcome)
	}
}

func TestWalledReplyRefusesFreeText(t *testing.T) {
	e := newEnvWalled(t, "client")
	it := e.post(t, walledNode, typed("12")).Item
	for _, body := range []string{"the build failed on the client's server", "ob-12", "ob-12 blocked please help", "OB-12 blocked", ""} {
		_, err := e.b.Reply(context.Background(), walledNode, it.ID, body, bus.SeverityNormal, bus.UrgencyDigest)
		wantCode(t, err, bus.CodeInvalid)
	}
	if len(e.messages(t, it.ID)) != 0 {
		t.Error("a refused reply stored a message")
	}
	m, err := e.b.Reply(context.Background(), walledNode, it.ID, "ob-12 blocked", bus.SeverityNormal, bus.UrgencyDigest)
	if err != nil || m.Body != "ob-12 blocked" {
		t.Fatalf("typed reply = %+v, %v", m, err)
	}
}

func TestIntoWalledSwarmOnlyGoOrHold(t *testing.T) {
	e := newEnvWalled(t, "client")
	it := e.post(t, walledNode, typed("12")).Item
	toClient := func(title string) bus.Draft {
		return bus.Draft{Kind: bus.KindNotice, Severity: bus.SeverityNormal, To: "client", Title: title, Action: bus.ActionNone, Urgency: bus.UrgencyDigest}
	}
	for _, title := range []string{"yes, go ahead", "go", "go sl:", "go sl:12 now", "Go sl:12", "go  sl:12", "stop sl:12", "go sl:12\nhold sl:13"} {
		_, err := e.b.Post(context.Background(), duet, toClient(title))
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != "title" {
			t.Errorf("post %q: What = %q", title, be.What)
		}
		_, err = e.b.Reply(context.Background(), duet, it.ID, title, bus.SeverityNormal, bus.UrgencyDigest)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != "body" {
			t.Errorf("reply %q: What = %q", title, be.What)
		}
	}
	d := toClient("go sl:12")
	d.Summary = "with words"
	_, err := e.b.Post(context.Background(), duet, d)
	wantCode(t, err, bus.CodeInvalid)
	d = toClient("go sl:12")
	d.Handle = "go-12"
	_, err = e.b.Post(context.Background(), duet, d)
	wantCode(t, err, bus.CodeInvalid)
	if len(e.messages(t, it.ID)) != 0 || len(e.events(t)) != 1 {
		t.Error("a refused call stored something")
	}
	for _, title := range []string{"go sl:12", "hold sl:7"} {
		if r := e.post(t, duet, toClient(title)); r.Item.Swarm != "client" || r.Item.Title != title {
			t.Errorf("post %q = %+v", title, r.Item)
		}
		if _, err := e.b.Reply(context.Background(), duet, it.ID, title, bus.SeverityNormal, bus.UrgencyDigest); err != nil {
			t.Errorf("reply %q: %v", title, err)
		}
	}
}

func TestWalledApprovalRefusesRealSlug(t *testing.T) {
	e := newEnvWalled(t, "client")
	for _, c := range []struct {
		name, field string
		edit        func(*bus.Draft)
	}{
		{"real slug", "repo", func(d *bus.Draft) { d.Repo = "o/r" }},
		{"codename with a slash", "repo", func(d *bus.Draft) { d.Repo = "cn:o/alpha" }},
		{"codename too long", "repo", func(d *bus.Draft) { d.Repo = "cn:a" + strings.Repeat("b", 40) }},
		{"codename with capitals", "repo", func(d *bus.Draft) { d.Repo = "cn:Alpha" }},
		{"branch ref", "ref", func(d *bus.Draft) { d.Ref = "feature/x" }},
		{"real slug in the target", "target", func(d *bus.Draft) { d.Target = "o/r#7" }},
	} {
		d := walledRelease()
		c.edit(&d)
		_, err := e.b.Post(context.Background(), walledNode, d)
		if be := errWithCode(t, err, bus.CodeInvalid); be.What != c.field {
			t.Errorf("%s: What = %q, want %q", c.name, be.What, c.field)
		}
	}
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after refused approvals", n)
	}
	r := e.post(t, walledNode, walledRelease())
	if r.Item.Repo != "cn:alpha" || r.Item.Swarm != "client" {
		t.Errorf("accepted item = %+v: the codename must be stored as posted", r.Item)
	}
	m := e.post(t, walledNode, walledMerge())
	if m.Item.Repo != "cn:alpha" || m.Item.Ref != "pr:42" || m.Item.BaseRef != "main" || m.Item.Swarm != "client" {
		t.Errorf("walled merge approval = %+v: the codename and the PR id must be stored as posted", m.Item)
	}
}

// What the item was gated on at post time is what a later re-check sees (round 3, LOW-1).
func TestStoredWalledItemKeepsItsPolicyTarget(t *testing.T) {
	e := newEnvWalled(t, "client")
	for _, d := range []bus.Draft{walledRelease(), walledMerge()} {
		it := e.post(t, walledNode, d).Item
		tg := it.PolicyTarget()
		if tg.Repo != "cn/alpha" {
			t.Errorf("%s: PolicyTarget repo = %q, want cn/alpha", d.Action, tg.Repo)
		}
		if err := e.l.Approvable(it.Action, tg); err != nil {
			t.Errorf("%s: re-check on the stored item: %v", d.Action, err)
		}
	}
	d := bus.Draft{Action: bus.ActionMerge, Repo: "o/r", Ref: "7", BaseRef: "main"}
	if tg := d.PolicyTarget(); tg.Repo != "o/r" || tg.Ref != "main" {
		t.Errorf("a plain slug changed: %+v", tg)
	}
}

func TestWalledRefAndTargetRefusals(t *testing.T) {
	e := newEnvWalled(t, "client")
	// The ref cases run on a merge, the one action a PR id is valid for, so the ref grammar itself
	// decides them and the merge-only rule (the cases after them) cannot mask it.
	refuseOn := func(base func() bus.Draft, name, field string, edit func(*bus.Draft)) {
		t.Run(name, func(t *testing.T) {
			d := base()
			edit(&d)
			_, err := e.b.Post(context.Background(), walledNode, d)
			if be := errWithCode(t, err, bus.CodeInvalid); be.What != field {
				t.Errorf("What = %q, want %q", be.What, field)
			}
		})
	}
	refuse := func(name, field string, edit func(*bus.Draft)) { refuseOn(walledRelease, name, field, edit) }
	for _, c := range []struct{ name, v string }{
		{"letters in a PR id", "pr:4a"}, {"letters after digits", "pr:4a4"}, {"a letter alone", "pr:a"}, {"seven digits", "pr:1234567"}, {"bare pr:", "pr:"}, {"a branch name", "feature/x"},
		{"a URL", "https://github.com/o/r/pull/7"}, {"capital PR", "PR:42"}, {"pr with a space", "pr: 42"}, {"two PR ids", "pr:4,pr:5"},
		{"a bare number", "42"}, {"a pr: tag", "pr:42x"}, {"release with a letter", "release/1.x"},
	} {
		refuseOn(walledMerge, "ref "+c.name, "ref", func(d *bus.Draft) { d.Ref = c.v })
	}
	// A PR id is a ref for a merge only: for a release it would be gated as the tag 42.
	for _, a := range []bus.Action{bus.ActionRelease, bus.ActionPublicSync, bus.ActionDeploy, bus.ActionOther, bus.ActionNone} {
		refuse("pr: ref on a "+string(a), "ref", func(d *bus.Draft) { d.Action, d.Ref = a, "pr:42" })
	}
	for _, c := range []struct{ name, v string }{
		{"a PR id", "pr:42"}, {"a branch name", "feature/x"}, {"a tag", "v1.2.3"}, {"a URL", "https://github.com/o/r"}, {"a letter PR", "pr:4a"},
	} {
		refuse("base "+c.name, "base_ref", func(d *bus.Draft) { d.BaseRef = c.v })
	}
	for _, c := range []struct{ name, v string }{
		{"a URL", "https://example.com/deploy"}, {"prose", "the client's site"}, {"slug with a number", "o/r#7"}, {"a slug", "o/r"},
		{"capital codename", "cn:Alpha"}, {"seven-digit number", "cn:alpha#1234567"}, {"bare hash", "cn:alpha#"}, {"letters after hash", "cn:alpha#7a"},
		{"a hyphenated token outside the list", "bigclient-payroll-outage"}, {"a bare codename word", "alpha"}, {"capital env", "Prod"},
		{"env with a slash", "prod/x"}, {"env with a colon", "prod:x"}, {"env with a number", "prod#7"}, {"env with a trailing space", "prod "},
	} {
		refuse("target "+c.name, "target", func(d *bus.Draft) { d.Target = c.v })
	}
	refuse("a walled node's risk flag", "risk_flags", func(d *bus.Draft) { d.RiskFlags = []string{"touches-auth"} })
	refuse("a walled node's hyphenated risk flag", "risk_flags", func(d *bus.Draft) { d.RiskFlags = []string{"acme-corp", "invoice-dispute"} })
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after refused posts", n)
	}

	into := bus.Draft{Kind: bus.KindNotice, Severity: bus.SeverityNormal, To: "client", Title: "go sl:12", Action: bus.ActionNone,
		Urgency: bus.UrgencyDigest, RiskFlags: []string{"call-the-cfo"}}
	_, err := e.b.Post(context.Background(), duet, into)
	if be := errWithCode(t, err, bus.CodeInvalid); be.What != "risk_flags" {
		t.Errorf("Duet's flag into a walled swarm: What = %q", be.What)
	}
	if n := len(e.events(t)); n != 0 {
		t.Errorf("%d events after refused posts", n)
	}

	n := 20
	for _, c := range []struct{ ref, base, target string }{
		{"main", "", ""}, {"release/1.2.3", "", "prod"}, {"v1.2.3", "", "staging"}, {"main", "main", "preview"},
		{"release/2", "release/2", "dev"}, {"v0.0.1", "release/10.0", "cn:alpha"}, {"main", "main", "cn:alpha#7"}, {"main", "main", "cn:alpha#123456"},
	} {
		n++
		d := typed(strconv.Itoa(n))
		d.Ref, d.BaseRef, d.Target = c.ref, c.base, c.target
		e.post(t, walledNode, d)
	}
}

func TestUnwalledSwarmUnchanged(t *testing.T) {
	e := newEnvWalled(t, "client")
	d := bus.Draft{Kind: bus.KindQuestion, Severity: bus.SeverityNormal, To: "human", Handle: "free text",
		Title: "Which port should the staging server use?", Summary: "words", Detail: "more words",
		RequiredAction: "pick one", Link: "https://github.com/o/r/pull/1", Action: bus.ActionNone, Urgency: bus.UrgencyDigest}
	d.Handle = "q-1"
	it := e.post(t, node("ci-1", "crew"), d).Item
	if _, err := e.b.Reply(context.Background(), node("ci-1", "crew"), it.ID, "any prose at all", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Errorf("prose reply in an unwalled swarm: %v", err)
	}
	dd := d
	dd.To, dd.Handle = "crew", "q-2"
	e.post(t, duet, dd)
	if _, err := e.b.Reply(context.Background(), duet, it.ID, "Use 8080.", bus.SeverityNormal, bus.UrgencyDigest); err != nil {
		t.Errorf("Duet's prose into an unwalled swarm: %v", err)
	}
}

// A crew node replying in prose to a walled item's id gets the same not_found as for a missing
// id: the wall must not run before the visibility check, or the error would reveal the item.
func TestCrewProseReplyToWalledItemIsNotFound(t *testing.T) {
	e := newEnvWalled(t, "client")
	it := e.post(t, walledNode, typed("12")).Item
	crew := node("ci-1", "crew")
	_, errWalled := e.b.Reply(context.Background(), crew, it.ID, "plain prose, not typed", bus.SeverityNormal, bus.UrgencyDigest)
	_, errMissing := e.b.Reply(context.Background(), crew, bus.NewID("itm"), "plain prose, not typed", bus.SeverityNormal, bus.UrgencyDigest)
	a, b := errWithCode(t, errWalled, bus.CodeNotFound), errWithCode(t, errMissing, bus.CodeNotFound)
	same(t, "walled vs missing", *a, *b)
}

// The walled list comes from the deployment's Limits at construction and from nowhere else.
func TestWalledListOnlyFromConfig(t *testing.T) {
	e := newEnvWalled(t, "client")
	// Nothing in a post names a wall, and a principal's swarm is not one.
	prose := notice("p1")
	prose.Summary = "words"
	if _, err := e.b.Post(context.Background(), node("ci-1", "crew"), prose); err != nil {
		t.Fatalf("crew is not walled: %v", err)
	}
	// A caller that kept the slice it passed can't change the set afterwards.
	walled := []string{"client"}
	l := bus.Limits{WalledSwarms: walled}
	b := bus.New(e.d, e.now, l)
	walled[0] = "other"
	_, err := b.Post(context.Background(), walledNode, prose)
	wantCode(t, err, bus.CodeInvalid)
	if _, err := b.Post(context.Background(), node("o-1", "other"), prose); err != nil {
		t.Errorf("swarm other became walled through the caller's slice: %v", err)
	}
	// A bus built with no walled swarms walls nothing.
	if _, err := bus.New(e.d, e.now, bus.Limits{}).Post(context.Background(), walledNode, prose); err != nil {
		t.Errorf("no config, no wall: %v", err)
	}
}

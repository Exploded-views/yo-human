package bus

import (
	"strings"
	"testing"
)

func TestCheckWalledDraft(t *testing.T) {
	ok := Draft{Kind: KindNotice, Severity: SeverityNormal, To: "human", Handle: "ob-3", Title: "ob-3 ack", Action: ActionNone, Urgency: UrgencyDigest}
	if err := CheckWalledDraft(ok); err != nil {
		t.Fatalf("typed draft: %v", err)
	}
	for _, c := range []struct {
		field string
		edit  func(*Draft)
	}{
		{"handle", func(d *Draft) { d.Handle = "ob-" }},
		{"handle", func(d *Draft) { d.Handle = "ob-3 " }},
		{"title", func(d *Draft) { d.Title = "ob-3 ack\n" }},
		{"title", func(d *Draft) { d.Title = "ob-3 ack sha256:" + strings.Repeat("A", 64) + " bytes:1" }},
		{"title", func(d *Draft) { d.Title = "ob-3 ack sha256:" + strings.Repeat("a", 63) + " bytes:1" }},
		{"repo", func(d *Draft) { d.Repo = "cn:" }},
		{"repo", func(d *Draft) { d.Repo = "cn:1alpha" }},
		{"ref", func(d *Draft) { d.Ref = "release/1.2-x" }},
		{"ref", func(d *Draft) { d.Ref = "v1.2" }},
		{"ref", func(d *Draft) { d.Ref = "v1.2.3-rc1" }},
		{"base_ref", func(d *Draft) { d.BaseRef = "develop" }},
		{"target", func(d *Draft) { d.Target = "Prod" }},
		{"risk_flags", func(d *Draft) { d.RiskFlags = []string{"touches-auth"} }},
		{"ref", func(d *Draft) { d.Action, d.Ref = ActionMerge, "pr:1234567" }},
		{"ref", func(d *Draft) { d.Action, d.Ref = ActionMerge, "pr:4a" }},
		{"ref", func(d *Draft) { d.Action, d.Ref = ActionMerge, "pr:0" }},
		{"ref", func(d *Draft) { d.Action, d.Ref = ActionMerge, "pr:01" }},
		{"ref", func(d *Draft) { d.Action, d.Ref = ActionMerge, "pr:000001" }},
		{"base_ref", func(d *Draft) { d.BaseRef = "pr:42" }},
		{"ref", func(d *Draft) { d.Ref = "pr:42" }}, // the action is none, not merge
		{"ref", func(d *Draft) { d.Action, d.Ref = ActionRelease, "pr:42" }},
	} {
		d := ok
		c.edit(&d)
		be, isBus := AsError(CheckWalledDraft(d))
		if !isBus || be.Code != CodeInvalid || be.What != c.field {
			t.Errorf("%s edit: got %+v", c.field, be)
		}
	}
	for _, edit := range []func(*Draft){
		func(d *Draft) { d.Repo, d.Ref, d.BaseRef, d.Target = "cn:alpha-1", "main", "release/1.20.3", "prod" },
		func(d *Draft) { d.Ref = "v10.0.1" },
		func(d *Draft) { d.Target = "cn:alpha#7" },
		func(d *Draft) { d.Action, d.Ref, d.BaseRef = ActionMerge, "pr:42", "main" },
		func(d *Draft) { d.Action, d.Ref, d.BaseRef = ActionMerge, "pr:1", "release/2" },
		func(d *Draft) { d.Action, d.Ref = ActionMerge, "pr:123456" },
	} {
		d := ok
		edit(&d)
		if err := CheckWalledDraft(d); err != nil {
			t.Errorf("allowed draft %+v: %v", d, err)
		}
	}
}

// The bool names the direction as the plan does: fromWalledNode is true for a body a walled
// node wrote (the typed grammar) and false for a body going into the walled swarm (go or hold).
func TestCheckWalledMessage(t *testing.T) {
	for _, c := range []struct {
		body           string
		fromWalledNode bool
		ok             bool
	}{
		{"ob-3 blocked", true, true}, {"ob-3 needs:none", true, true}, {"go sl:3", true, false},
		{"ob-3 blocked ", true, false}, {"", true, false},
		{"go sl:3", false, true}, {"hold sl:30", false, true}, {"ob-3 blocked", false, false},
		{"go sl:3 ", false, false}, {"go sl:-3", false, false}, {"go sl:03x", false, false}, {"", false, false},
	} {
		err := CheckWalledMessage(c.body, c.fromWalledNode)
		if c.ok != (err == nil) {
			t.Errorf("CheckWalledMessage(%q, fromWalledNode=%v) = %v, want ok=%v", c.body, c.fromWalledNode, err, c.ok)
		}
		if err != nil {
			if be, isBus := AsError(err); !isBus || be.Code != CodeInvalid || be.What != "body" {
				t.Errorf("CheckWalledMessage(%q, %v) error = %+v", c.body, c.fromWalledNode, err)
			}
		}
	}
}

// A PR id is a number from 1 without a leading zero, and the error says so (round 4, NIT-2): the
// wall accepts what Validate's PR number rule accepts.
func TestWalledPRRefGrammarIsNamedInTheError(t *testing.T) {
	for _, ref := range []string{"pr:0", "pr:01", "pr:000001", "pr:", "pr:1234567"} {
		err := CheckWalledDraft(Draft{Handle: "ob-3", Title: "ob-3 ack", Action: ActionMerge, Ref: ref})
		be, ok := AsError(err)
		if !ok || be.What != "ref" || !strings.Contains(be.Valid, "no leading zero") {
			t.Errorf("%s: error = %+v", ref, err)
		}
	}
	for _, ref := range []string{"pr:1", "pr:9", "pr:10", "pr:100000", "pr:999999"} {
		if err := CheckWalledDraft(Draft{Handle: "ob-3", Title: "ob-3 ack", Action: ActionMerge, Ref: ref}); err != nil {
			t.Errorf("%s: %v", ref, err)
		}
	}
}

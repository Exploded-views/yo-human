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

// Every digit field of the walled grammar is bounded (round 4, L2): a long digit run can carry text,
// so ids and counts are 1 to 6 digits and a semver part 1 to 4. Ids (ob-N, sl:N) start 1-9; a
// byte count and a release number may be 0 itself but never have a leading zero.
func TestWalledDigitFieldsAreBounded(t *testing.T) {
	d9 := func(n int) string { return strings.Repeat("9", n) }
	sha := "sha256:" + strings.Repeat("a", 64)
	draft := func(edit func(*Draft)) error {
		d := Draft{Handle: "ob-3", Title: "ob-3 ack", Action: ActionNone}
		edit(&d)
		return CheckWalledDraft(d)
	}
	msg := func(into bool, body string) error { return CheckWalledMessage(body, !into) }
	for _, c := range []struct {
		name         string
		ok, tooLong  func() error
		zeroOrLeader []func() error // refused
		zeroOK       func() error   // accepted, when 0 itself is a value
	}{
		{"handle ob-N", func() error { return draft(func(d *Draft) { d.Handle = "ob-" + d9(6) }) },
			func() error { return draft(func(d *Draft) { d.Handle = "ob-" + d9(7) }) },
			[]func() error{func() error { return draft(func(d *Draft) { d.Handle = "ob-0" }) }, func() error { return draft(func(d *Draft) { d.Handle = "ob-01" }) }}, nil},
		{"title ob-N", func() error { return draft(func(d *Draft) { d.Title = "ob-" + d9(6) + " ack" }) },
			func() error { return draft(func(d *Draft) { d.Title = "ob-" + d9(7) + " ack" }) },
			[]func() error{func() error { return draft(func(d *Draft) { d.Title = "ob-0 ack" }) }, func() error { return draft(func(d *Draft) { d.Title = "ob-007 ack" }) }}, nil},
		{"title bytes:N", func() error { return draft(func(d *Draft) { d.Title = "ob-3 done " + sha + " bytes:" + d9(6) }) },
			func() error { return draft(func(d *Draft) { d.Title = "ob-3 done " + sha + " bytes:" + d9(7) }) },
			[]func() error{func() error { return draft(func(d *Draft) { d.Title = "ob-3 done " + sha + " bytes:01" }) }},
			func() error { return draft(func(d *Draft) { d.Title = "ob-3 done " + sha + " bytes:0" }) }},
		{"message ob-N", func() error { return msg(false, "ob-"+d9(6)+" ack") }, func() error { return msg(false, "ob-"+d9(7)+" ack") },
			[]func() error{func() error { return msg(false, "ob-0 ack") }, func() error { return msg(false, "ob-01 ack") }}, nil},
		{"message bytes:N", func() error { return msg(false, "ob-3 done "+sha+" bytes:"+d9(6)) }, func() error { return msg(false, "ob-3 done "+sha+" bytes:"+d9(7)) },
			[]func() error{func() error { return msg(false, "ob-3 done "+sha+" bytes:00") }},
			func() error { return msg(false, "ob-3 done "+sha+" bytes:0") }},
		{"sl:N", func() error { return msg(true, "go sl:"+d9(6)) }, func() error { return msg(true, "hold sl:"+d9(7)) },
			[]func() error{func() error { return msg(true, "go sl:0") }, func() error { return msg(true, "hold sl:01") }}, nil},
		{"release/N", func() error { return draft(func(d *Draft) { d.Ref = "release/" + d9(6) + "." + d9(6) + ".0" }) },
			func() error { return draft(func(d *Draft) { d.Ref = "release/" + d9(7) }) },
			[]func() error{func() error { return draft(func(d *Draft) { d.Ref = "release/01" }) }, func() error { return draft(func(d *Draft) { d.Ref = "release/1.02" }) }},
			func() error { return draft(func(d *Draft) { d.Ref = "release/0.9" }) }},
		{"release/N part", func() error { return draft(func(d *Draft) { d.Ref = "release/1." + d9(6) }) },
			func() error { return draft(func(d *Draft) { d.Ref = "release/1." + d9(7) }) }, nil, nil},
		{"base release/N", func() error { return draft(func(d *Draft) { d.BaseRef = "release/" + d9(6) }) },
			func() error { return draft(func(d *Draft) { d.BaseRef = "release/" + d9(7) }) },
			[]func() error{func() error { return draft(func(d *Draft) { d.BaseRef = "release/007" }) }}, nil},
		{"semver major", func() error { return draft(func(d *Draft) { d.Ref = "v" + d9(4) + ".1.1" }) },
			func() error { return draft(func(d *Draft) { d.Ref = "v" + d9(5) + ".1.1" }) }, nil, nil},
		{"semver minor", func() error { return draft(func(d *Draft) { d.Ref = "v1." + d9(4) + ".1" }) },
			func() error { return draft(func(d *Draft) { d.Ref = "v1." + d9(5) + ".1" }) }, nil, nil},
		{"semver patch", func() error { return draft(func(d *Draft) { d.Ref = "v1.1." + d9(4) }) },
			func() error { return draft(func(d *Draft) { d.Ref = "v1.1." + d9(5) }) },
			[]func() error{func() error { return draft(func(d *Draft) { d.Ref = "v1.1.01" }) }},
			func() error { return draft(func(d *Draft) { d.Ref = "v0.0.0" }) }},
	} {
		if err := c.ok(); err != nil {
			t.Errorf("%s: the longest value is refused: %v", c.name, err)
		}
		if err := c.tooLong(); err == nil {
			t.Errorf("%s: one digit more is accepted", c.name)
		}
		for i, z := range c.zeroOrLeader {
			if err := z(); err == nil {
				t.Errorf("%s: zero or leading-zero case %d is accepted", c.name, i)
			}
		}
		if c.zeroOK != nil {
			if err := c.zeroOK(); err != nil {
				t.Errorf("%s: 0 is refused: %v", c.name, err)
			}
		}
	}
}

// A release name has at most three dotted parts, major.minor.patch (round 5, MED-1): without a cap
// a ref or base_ref could carry a dozen numbers, which is text. The error names the grammar.
func TestWalledReleaseHasAtMostThreeParts(t *testing.T) {
	for _, v := range []string{"release/1", "release/1.2", "release/1.2.3", "release/0.0.0", "release/999999.999999.999999"} {
		for _, d := range []Draft{{Handle: "ob-3", Title: "ob-3 ack", Ref: v}, {Handle: "ob-3", Title: "ob-3 ack", BaseRef: v}} {
			if err := CheckWalledDraft(d); err != nil {
				t.Errorf("%s: %v", v, err)
			}
		}
	}
	for _, v := range []string{"release/1.2.3.4", "release/1.2.3.4.5", "release/1.1.1.1.1.1.1.1.1.1.1.1.1"} {
		for field, d := range map[string]Draft{"ref": {Handle: "ob-3", Title: "ob-3 ack", Ref: v}, "base_ref": {Handle: "ob-3", Title: "ob-3 ack", BaseRef: v}} {
			be, ok := AsError(CheckWalledDraft(d))
			if !ok || be.What != field || !strings.Contains(be.Valid, "at most 3 parts") {
				t.Errorf("%s as %s: error = %+v, want %s naming the 3-part grammar", v, field, be, field)
			}
		}
	}
}

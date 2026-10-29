package bus

import (
	"strings"
	"testing"
)

func TestCheckWalledDraft(t *testing.T) {
	ok := Draft{Kind: KindNotice, Severity: SeverityNormal, To: "human", Handle: "ob-3", Title: "ob-3 ack", Action: ActionNone,
		RiskFlags: []string{"touches-auth"}}
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
	} {
		d := ok
		edit(&d)
		if err := CheckWalledDraft(d); err != nil {
			t.Errorf("allowed draft %+v: %v", d, err)
		}
	}
}

func TestCheckWalledMessage(t *testing.T) {
	for _, c := range []struct {
		body       string
		intoWalled bool
		ok         bool
	}{
		{"ob-3 blocked", false, true}, {"ob-3 needs:none", false, true}, {"go sl:3", false, false},
		{"ob-3 blocked ", false, false}, {"", false, false},
		{"go sl:3", true, true}, {"hold sl:30", true, true}, {"ob-3 blocked", true, false},
		{"go sl:3 ", true, false}, {"go sl:-3", true, false}, {"go sl:03x", true, false}, {"", true, false},
	} {
		err := CheckWalledMessage(c.body, c.intoWalled)
		if c.ok != (err == nil) {
			t.Errorf("CheckWalledMessage(%q, %v) = %v, want ok=%v", c.body, c.intoWalled, err, c.ok)
		}
		if err != nil {
			if be, isBus := AsError(err); !isBus || be.Code != CodeInvalid || be.What != "body" {
				t.Errorf("CheckWalledMessage(%q, %v) error = %+v", c.body, c.intoWalled, err)
			}
		}
	}
}

package policy

import (
	"reflect"
	"testing"
)

var categories = []string{"merge", "release", "deploy", "public-sync", "force-push", "delete", "spend", "secrets", "money"}

func mustParse(t *testing.T, s string) Policy {
	t.Helper()
	p, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("Parse(%s): %v", s, err)
	}
	return p
}

func TestEmptyPolicyDeniesEverything(t *testing.T) {
	p := mustParse(t, `{"v": 1, "rules": []}`)
	for _, a := range categories {
		if got := p.Decide(a, Target{Repo: "example-org/r", Ref: "main"}); got != Deny {
			t.Errorf("Decide(%s) on an empty policy = %q, want deny", a, got)
		}
	}
}

func TestUnlistedRepoDenied(t *testing.T) {
	p := mustParse(t, `{"v": 1, "rules": [{"action": "merge", "repo": "example-org/listed", "ref": "main", "mode": "passkey"}]}`)
	if got := p.Decide("merge", Target{Repo: "example-org/other", Ref: "main"}); got != Deny {
		t.Errorf("unlisted repo: %q, want deny", got)
	}
	if got := p.Decide("merge", Target{Repo: "example-org/listed", Ref: "main"}); got != Passkey {
		t.Errorf("listed repo: %q, want passkey", got)
	}
}

func TestNeverListNotOverridable(t *testing.T) {
	for _, a := range []string{"force-push", "delete", "spend", "secrets", "money"} {
		file := `{"v": 1, "rules": [{"action": "` + a + `", "repo": "*", "mode": "passkey"}]}`
		if _, err := Parse([]byte(file)); err == nil {
			t.Errorf("Parse accepted a %s rule", a)
		}
		hand := Policy{V: 1, Rules: []Rule{{Action: a, Repo: "*", Mode: Passkey}, {Action: a, Repo: "*", Mode: Standing}}}
		if got := hand.Decide(a, Target{Repo: "x"}); got != Deny {
			t.Errorf("hand-built %s: Decide = %q, want deny", a, got)
		}
		if g := hand.Gates(); len(g) != 0 {
			t.Errorf("hand-built %s: Gates = %v, want none", a, g)
		}
	}
}

func TestParseRefusesBadFiles(t *testing.T) {
	bad := map[string]string{
		"unknown field":      `{"v": 1, "rules": [], "extra": 1}`,
		"unknown rule field": `{"v": 1, "rules": [{"action": "merge", "repo": "a/b", "mode": "passkey", "x": 1}]}`,
		"version 2":          `{"v": 2, "rules": []}`,
		"no version":         `{"rules": []}`,
		"unknown action":     `{"v": 1, "rules": [{"action": "other", "repo": "a/b", "mode": "passkey"}]}`,
		"unknown mode":       `{"v": 1, "rules": [{"action": "merge", "repo": "a/b", "mode": "maybe"}]}`,
		"trailing data":      `{"v": 1, "rules": []} {"v": 1}`,
		"not json":           `v: 1`,
	}
	for name, file := range bad {
		if _, err := Parse([]byte(file)); err == nil {
			t.Errorf("%s: Parse accepted %s", name, file)
		}
	}
}

func TestParseRefusesBadPatterns(t *testing.T) {
	for _, r := range []string{
		`"repo": "a/b?"`, `"repo": "a/[b]"`, `"repo": "a/\\*"`, `"env": "["`,
		`"repo": "a/b", "ref": "refs/heads/main"`,
	} {
		file := `{"v": 1, "rules": [{"action": "merge", ` + r + `, "mode": "passkey"}]}`
		if _, err := Parse([]byte(file)); err == nil {
			t.Errorf("Parse accepted %s", r)
		}
	}
}

func TestPasskeyBeatsStandingRegardlessOfOrder(t *testing.T) {
	standing := Rule{Action: "merge", Repo: "example-org/*", Ref: "main", Mode: Standing}
	passkey := Rule{Action: "merge", Repo: "example-org/owner-kept-repo", Ref: "main", Mode: Passkey}
	kept := Target{Repo: "example-org/owner-kept-repo", Ref: "main"}
	for _, rules := range [][]Rule{{standing, passkey}, {passkey, standing}} {
		p := Policy{V: 1, Rules: rules}
		if got := p.Decide("merge", kept); got != Passkey {
			t.Errorf("order %v: Decide = %q, want passkey", rules, got)
		}
		if got := p.Decide("merge", Target{Repo: "example-org/other", Ref: "main"}); got != Standing {
			t.Errorf("order %v: other repo = %q, want standing", rules, got)
		}
		if g := p.Gates(); !reflect.DeepEqual(g, []Rule{passkey}) {
			t.Errorf("order %v: Gates = %v, want the passkey rule", rules, g)
		}
	}
	deny := Rule{Action: "merge", Repo: "example-org/owner-kept-repo", Ref: "*", Mode: Deny}
	for _, rules := range [][]Rule{{deny, standing, passkey}, {passkey, standing, deny}} {
		p := Policy{V: 1, Rules: rules}
		if got := p.Decide("merge", kept); got != Deny {
			t.Errorf("deny %v: Decide = %q, want deny", rules, got)
		}
		if g := p.Gates(); len(g) != 0 {
			t.Errorf("deny %v: Gates = %v, want none (the passkey rule never wins)", rules, g)
		}
	}
}

func TestShadowedRuleReported(t *testing.T) {
	broad := Rule{Action: "release", Repo: "example-org/*", Ref: "*", Mode: Passkey}
	covered := Rule{Action: "release", Repo: "example-org/r", Ref: "v*", Mode: Standing}
	partial := Rule{Action: "release", Repo: "*/r", Ref: "v*", Mode: Standing}
	p := Policy{V: 1, Rules: []Rule{covered, broad, partial}}
	if got := p.Shadowed(); !reflect.DeepEqual(got, []Rule{covered}) {
		t.Errorf("Shadowed = %v, want only the covered standing rule", got)
	}
	if got := p.Decide("release", Target{Repo: "other-org/r", Ref: "v1"}); got != Standing {
		t.Errorf("partial rule's own target = %q, want standing", got)
	}
}

func TestGatesAgreeWithDecideUnderUnionOfDenies(t *testing.T) {
	// Two deny rules that each cover half of the passkey rule's refs leave a ref neither matches.
	gate := Rule{Action: "release", Repo: "a/b", Ref: "*", Mode: Passkey}
	p := Policy{V: 1, Rules: []Rule{gate, {Action: "release", Repo: "a/b", Ref: "v*", Mode: Deny}, {Action: "release", Repo: "a/b", Ref: "x*", Mode: Deny}}}
	if g := p.Gates(); !reflect.DeepEqual(g, []Rule{gate}) {
		t.Errorf("Gates = %v, want the gate (ref \"w1\" reaches it)", g)
	}
	if got := p.Decide("release", Target{Repo: "a/b", Ref: "w1"}); got != Passkey {
		t.Errorf("Decide(w1) = %q, want passkey", got)
	}
}

func TestRepoCaseFolded(t *testing.T) {
	p := Policy{V: 1, Rules: []Rule{{Action: "merge", Repo: "Example-Org/Repo", Ref: "main", Mode: Passkey}}}
	if got := p.Decide("merge", Target{Repo: "example-org/REPO", Ref: "main"}); got != Passkey {
		t.Errorf("repo case: %q, want passkey", got)
	}
	if got := p.Decide("merge", Target{Repo: "example-org/repo", Ref: "MAIN"}); got != Deny {
		t.Errorf("ref is not case-folded: %q, want deny", got)
	}
}

func TestEmptyPatternMatchesOnlyEmpty(t *testing.T) {
	p := Policy{V: 1, Rules: []Rule{{Action: "deploy", Repo: "a/b", Env: "prod", Mode: Passkey}}}
	if got := p.Decide("deploy", Target{Repo: "a/b", Env: "prod"}); got != Passkey {
		t.Errorf("empty ref pattern, empty ref: %q, want passkey", got)
	}
	if got := p.Decide("deploy", Target{Repo: "a/b", Ref: "main", Env: "prod"}); got != Deny {
		t.Errorf("empty ref pattern, ref set: %q, want deny", got)
	}
}

func TestStarDoesNotMatchSlash(t *testing.T) {
	p := Policy{V: 1, Rules: []Rule{{Action: "merge", Repo: "*", Ref: "*", Mode: Passkey}}}
	if got := p.Decide("merge", Target{Repo: "a/b", Ref: "main"}); got != Deny {
		t.Errorf("repo * vs a/b: %q, want deny", got)
	}
	if got := p.Decide("merge", Target{Repo: "ab", Ref: "feature/x"}); got != Deny {
		t.Errorf("ref * vs feature/x: %q, want deny", got)
	}
}

func TestInvalidModeInHandBuiltPolicyDenies(t *testing.T) {
	p := Policy{V: 1, Rules: []Rule{{Action: "merge", Repo: "a/b", Ref: "main", Mode: Passkey}, {Action: "merge", Repo: "a/b", Ref: "main", Mode: "bogus"}}}
	if got := p.Decide("merge", Target{Repo: "a/b", Ref: "main"}); got != Deny {
		t.Errorf("a bogus mode = %q, want deny", got)
	}
}

func TestTargetFor(t *testing.T) {
	cases := []struct {
		action string
		want   Target
	}{
		{"merge", Target{Repo: "o/r", Ref: "main"}},
		{"release", Target{Repo: "o/r", Ref: "v1.2.3"}},
		{"public-sync", Target{Repo: "o/r", Ref: "v1.2.3"}},
		{"deploy", Target{Repo: "o/r", Env: "prod"}},
	}
	for _, c := range cases {
		if got := TargetFor(c.action, "o/r", "v1.2.3", "main", "prod"); got != c.want {
			t.Errorf("TargetFor(%s) = %+v, want %+v", c.action, got, c.want)
		}
	}
}

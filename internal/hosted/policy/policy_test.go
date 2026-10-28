package policy

import (
	"reflect"
	"strings"
	"testing"
)

var allCategories = []string{"merge", "release", "deploy", "public-sync", "force-push", "delete", "spend", "secrets", "money"}

func mustParse(t *testing.T, s string) Policy {
	t.Helper()
	p, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("Parse(%s): %v", s, err)
	}
	return p
}

func gates(t *testing.T, p Policy) []Rule {
	t.Helper()
	g, err := p.Gates()
	if err != nil {
		t.Fatalf("Gates: %v", err)
	}
	return g
}

func shadowed(t *testing.T, p Policy) []Rule {
	t.Helper()
	sh, err := p.Shadowed()
	if err != nil {
		t.Fatalf("Shadowed: %v", err)
	}
	return sh
}

func TestEmptyPolicyDeniesEverything(t *testing.T) {
	p := mustParse(t, `{"v": 1, "rules": []}`)
	for _, a := range allCategories {
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
		if g := gates(t, hand); len(g) != 0 {
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
		"v as a string":      `{"v": "1", "rules": []}`,
		"truncated":          `{"v": 1, `,
	}
	for name, file := range bad {
		if _, err := Parse([]byte(file)); err == nil {
			t.Errorf("%s: Parse accepted %s", name, file)
		}
	}
}

func TestParseRefusesBadPatterns(t *testing.T) {
	for _, r := range []string{
		`"repo": "a/b?", "ref": "main"`, `"repo": "a/[b]", "ref": "main"`, `"repo": "a/\\*", "ref": "main"`,
		`"repo": "a/b", "ref": "["`, `"repo": "a/b", "ref": "m?in"`, `"repo": "a/b", "ref": "\u00e9"`,
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
		if g := gates(t, p); !reflect.DeepEqual(g, []Rule{passkey}) {
			t.Errorf("order %v: Gates = %v, want the passkey rule", rules, g)
		}
	}
	deny := Rule{Action: "merge", Repo: "example-org/owner-kept-repo", Ref: "*", Mode: Deny}
	for _, rules := range [][]Rule{{deny, standing, passkey}, {passkey, standing, deny}} {
		p := Policy{V: 1, Rules: rules}
		if got := p.Decide("merge", kept); got != Deny {
			t.Errorf("deny %v: Decide = %q, want deny", rules, got)
		}
		if g := gates(t, p); len(g) != 0 {
			t.Errorf("deny %v: Gates = %v, want none (the passkey rule never wins)", rules, g)
		}
	}
}

func TestShadowedRuleReported(t *testing.T) {
	broad := Rule{Action: "release", Repo: "example-org/*", Ref: "*", Mode: Passkey}
	covered := Rule{Action: "release", Repo: "example-org/r", Ref: "v*", Mode: Standing}
	partial := Rule{Action: "release", Repo: "*/r", Ref: "v*", Mode: Standing}
	p := Policy{V: 1, Rules: []Rule{covered, broad, partial}}
	if got := shadowed(t, p); !reflect.DeepEqual(got, []Rule{covered}) {
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
	if g := gates(t, p); !reflect.DeepEqual(g, []Rule{gate}) {
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

func TestParseRefusesMidOrMultipleStarInRef(t *testing.T) {
	for _, action := range []string{"merge", "release", "public-sync"} {
		for _, mode := range []string{"passkey", "standing", "deny"} {
			for _, ref := range []string{"release/*/hotfix", "a*b", "**", "*x*", "*x", "v*.*"} {
				file := `{"v": 1, "rules": [{"action": "` + action + `", "repo": "a/b", "ref": "` + ref + `", "mode": "` + mode + `"}]}`
				_, err := Parse([]byte(file))
				if err == nil || !strings.Contains(err.Error(), "only as the last character") {
					t.Errorf("%s %s ref %q: err = %v, want the trailing-* reason", action, mode, ref, err)
				}
			}
		}
	}
	for _, ref := range []string{"*", "release/*", "v*", "main", "v1.2.*"} {
		mustParse(t, `{"v": 1, "rules": [{"action": "release", "repo": "a/b", "ref": "`+ref+`", "mode": "passkey"}]}`)
	}
}

func TestParseCapsPatternLengths(t *testing.T) {
	for name, r := range map[string]string{
		"repo of 201": `"action": "merge", "repo": "a/` + strings.Repeat("b", 199) + `", "ref": "main"`,
		"ref of 101":  `"action": "merge", "repo": "a/b", "ref": "` + strings.Repeat("r", 101) + `"`,
		"env of 64":   `"action": "deploy", "repo": "a/b", "env": "` + strings.Repeat("e", 64) + `"`,
	} {
		if _, err := Parse([]byte(`{"v": 1, "rules": [{` + r + `, "mode": "deny"}]}`)); err == nil {
			t.Errorf("%s: Parse accepted it", name)
		}
	}
	mustParse(t, `{"v": 1, "rules": [{"action": "merge", "repo": "a/`+strings.Repeat("b", 198)+`", "ref": "`+strings.Repeat("r", 100)+`", "mode": "deny"}]}`)
	mustParse(t, `{"v": 1, "rules": [{"action": "deploy", "repo": "a/b", "env": "`+strings.Repeat("e", 63)+`", "mode": "deny"}]}`)
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

func TestWitnessAvoidsRunesInPatterns(t *testing.T) {
	// A deny rule naming the first private-use rune must not make that rune the witness, or the
	// gate's witness would match the deny rule and the gate would wrongly drop out.
	gate := Rule{Action: "release", Repo: "a/b", Ref: "*", Mode: Passkey}
	p := Policy{V: 1, Rules: []Rule{gate, {Action: "release", Repo: "a/b", Ref: "\ue000", Mode: Deny}}}
	if g := gates(t, p); !reflect.DeepEqual(g, []Rule{gate}) {
		t.Errorf("Gates = %v, want the gate", g)
	}
}

func TestParseRefusesUnreachableRules(t *testing.T) {
	for name, r := range map[string]string{
		"repo *":                  `"action": "merge", "repo": "*", "ref": "main"`,
		"repo .git":               `"action": "merge", "repo": "o/r.git", "ref": "main"`,
		"repo .GIT":               `"action": "merge", "repo": "o/r.GIT", "ref": "main"`,
		"repo pattern *.git":      `"action": "merge", "repo": "o/*.git", "ref": "main"`,
		"owner with a leading -":  `"action": "merge", "repo": "-o/r", "ref": "main"`,
		"no repo":                 `"action": "merge", "ref": "main"`,
		"repo with no name":       `"action": "merge", "repo": "a/", "ref": "main"`,
		"repo with no owner":      `"action": "merge", "repo": "/b", "ref": "main"`,
		"repo with two slashes":   `"action": "merge", "repo": "a/b/c", "ref": "main"`,
		"merge with no ref":       `"action": "merge", "repo": "a/b"`,
		"merge with an env":       `"action": "merge", "repo": "a/b", "ref": "main", "env": "prod"`,
		"release with no ref":     `"action": "release", "repo": "a/b"`,
		"public-sync with an env": `"action": "public-sync", "repo": "a/b", "ref": "v*", "env": "prod"`,
		"deploy with no env":      `"action": "deploy", "repo": "a/b"`,
		"deploy with a ref":       `"action": "deploy", "repo": "a/b", "ref": "main", "env": "prod"`,
		"deploy env upper case":   `"action": "deploy", "repo": "a/b", "env": "Prod"`,
		"deploy env with a space": `"action": "deploy", "repo": "a/b", "env": "prod 2"`,
	} {
		file := `{"v": 1, "rules": [{` + r + `, "mode": "deny"}]}`
		if _, err := Parse([]byte(file)); err == nil {
			t.Errorf("%s: Parse accepted %s", name, file)
		}
	}
	for _, r := range []string{
		`"action": "merge", "repo": "*/*", "ref": "*"`,
		`"action": "deploy", "repo": "a/b", "env": "prod-*"`,
		`"action": "release", "repo": "a/my.repo", "ref": "v*"`,
		`"action": "release", "repo": "a/x.github", "ref": "v*"`,
		`"action": "release", "repo": "*/r", "ref": "v*"`,
	} {
		mustParse(t, `{"v": 1, "rules": [{`+r+`, "mode": "deny"}]}`)
	}
}

func TestParseRefusesDuplicateAndFoldedKeys(t *testing.T) {
	for name, file := range map[string]string{
		"duplicate mode":  `{"v": 1, "rules": [{"action": "merge", "repo": "a/b", "ref": "main", "mode": "deny", "mode": "passkey"}]}`,
		"MODE after mode": `{"v": 1, "rules": [{"action": "merge", "repo": "a/b", "ref": "main", "mode": "deny", "MODE": "passkey"}]}`,
		"upper-case V":    `{"V": 1, "rules": []}`,
		"long s in rules": `{"v": 1, "rule\u017f": []}`,
		"duplicate v":     `{"v": 1, "v": 1, "rules": []}`,
		"duplicate rules": `{"v": 1, "rules": [], "rules": [{"action": "merge", "repo": "a/b", "ref": "main", "mode": "passkey"}]}`,
		"Kelvin in a key": `{"v": 1, "rules": [{"action": "merge", "repo": "a/b", "ref": "main", "mode": "deny", "\u212aey": 1}]}`,
	} {
		if _, err := Parse([]byte(file)); err == nil {
			t.Errorf("%s: Parse accepted %s", name, file)
		}
	}
}

func TestFreshRuneBoundedFailsClosed(t *testing.T) {
	// A hand-built policy whose patterns use every private-use rune leaves no witness. Gates and
	// Shadowed then return an error rather than guess: a false "shadowed" could lead the owner to
	// delete a live rule.
	var all []rune
	for z := rune(0xE000); z <= 0xF8FF; z++ {
		all = append(all, z)
	}
	gate := Rule{Action: "release", Repo: "a/b", Ref: "*", Mode: Passkey}
	p := Policy{V: 1, Rules: []Rule{gate, {Action: "release", Repo: "a/b", Ref: string(all), Mode: Deny}}}
	if _, ok := p.freshRune(); ok {
		t.Error("freshRune found a rune outside U+E000-U+F8FF or one the policy uses")
	}
	if g, err := p.Gates(); err == nil {
		t.Errorf("Gates = %v, nil; want an error", g)
	}
	if sh, err := p.Shadowed(); err == nil {
		t.Errorf("Shadowed = %v, nil; want an error", sh)
	}
}

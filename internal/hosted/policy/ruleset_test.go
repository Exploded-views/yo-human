package policy

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestStarMatchesSlash(t *testing.T) {
	// A ref * is "every ref": it must cover slashed branches and tags, or a deny-all on * is
	// dodged by any rule naming release/*.
	p := Policy{V: 1, Rules: []Rule{
		{Action: "merge", Repo: "o/r", Ref: "*", Mode: Deny},
		{Action: "merge", Repo: "o/r", Ref: "release/*", Mode: Passkey},
	}}
	for _, ref := range []string{"release/1.2", "main", "feature/a/b"} {
		if got := p.Decide("merge", Target{Repo: "o/r", Ref: ref}); got != Deny {
			t.Errorf("deny * vs %s: %q, want deny", ref, got)
		}
	}
	if g := gates(t, p); len(g) != 0 {
		t.Errorf("Gates = %v, want none (the deny-all covers release/*)", g)
	}
	q := Policy{V: 1, Rules: []Rule{{Action: "release", Repo: "o/*", Ref: "v*", Mode: Passkey}}}
	for target, want := range map[Target]Mode{
		{Repo: "o/r", Ref: "v1/rc"}: Passkey, {Repo: "o/x/y", Ref: "v1"}: Passkey, {Repo: "p/r", Ref: "v1"}: Deny,
	} {
		if got := q.Decide("release", target); got != want {
			t.Errorf("Decide(%+v) = %q, want %q", target, got, want)
		}
	}
}

func TestGlob(t *testing.T) {
	for _, c := range []struct {
		pat, v string
		want   bool
	}{
		{"*", "", true}, {"*", "a/b", true}, {"a*", "a", true}, {"a*b", "a/x/b", true}, {"a*b", "a/x/c", false},
		{"*a*", "bab", true}, {"*a*", "bbb", false}, {"a*a", "a", false}, {"**", "x", true}, {"main", "main", true},
		{"main", "Main", false}, {"v*", "V1", false}, {"a*b*c", "abxbyc", true}, {"a*b*c", "acb", false},
	} {
		if got := glob(c.pat, c.v); got != c.want {
			t.Errorf("glob(%q, %q) = %v, want %v", c.pat, c.v, got, c.want)
		}
	}
}

func TestRulesetRefPatterns(t *testing.T) {
	for _, c := range []struct {
		action, ref string
		want        []string
	}{
		{"merge", "main", []string{"refs/heads/main"}},
		{"merge", "release/*", []string{"refs/heads/release/*", "refs/heads/release/*/**/*"}},
		{"merge", "*", []string{"refs/heads/*", "refs/heads/*/**/*"}},
		{"release", "v*", []string{"refs/tags/v*", "refs/tags/v*/**/*"}},
		{"public-sync", "v1", []string{"refs/heads/v1", "refs/tags/v1"}},
		{"deploy", "", nil},
	} {
		if got := RulesetRefPatterns(c.action, c.ref); !reflect.DeepEqual(got, c.want) {
			t.Errorf("RulesetRefPatterns(%s, %q) = %q, want %q", c.action, c.ref, got, c.want)
		}
	}
}

// TestRulesetPatternsMatchGlob checks the emitted ruleset patterns against GitHub's matching, not
// against strings: testdata/fnmatch.tsv records, per row, Ruby File.fnmatch with FNM_PATHNAME
// (what GitHub documents) over the full ref. Go must emit the same patterns, and the policy's
// glob must agree with fnmatch on every row, so a ruleset covers exactly what Decide covers.
func TestRulesetPatternsMatchGlob(t *testing.T) {
	b, err := os.ReadFile("testdata/fnmatch.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("bad row %q", line)
		}
		kind, pat, emitted, ref, github := f[0], f[1], f[2], f[3], f[4] == "true"
		if got := strings.Join(RulesetRefPatterns(kind, pat), " "); got != emitted {
			t.Errorf("%s %q: emitted %q, table %q", kind, pat, got, emitted)
		}
		if got := glob(pat, ref); got != github {
			t.Errorf("%s %q vs %q: glob %v, GitHub fnmatch %v", kind, pat, ref, got, github)
		}
		rows++
	}
	if rows < 600 {
		t.Fatalf("only %d rows in testdata/fnmatch.tsv", rows)
	}
}

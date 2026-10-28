package policy

import "strings"

// glob reports whether v matches pat, where `*` matches any run of characters (`/` included)
// and every other character matches itself. Greedy with one backtrack point: linear in practice.
func glob(pat, v string) bool {
	p, s := []rune(pat), []rune(v)
	pi, si, star, mark := 0, 0, -1, 0
	for si < len(s) {
		switch {
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, si
			pi++
		case pi < len(p) && p[pi] == s[si]:
			pi, si = pi+1, si+1
		case star >= 0:
			mark++
			pi, si = star+1, mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// rulesetPrefixes are the full-ref prefixes a ruleset targets, per action. A public-sync ref may
// be a branch or a tag (the plan doesn't fix which), so it gets both: gating both is the safe
// side. Deploy has no ref.
var rulesetPrefixes = map[string][]string{
	"merge":       {"refs/heads/"},
	"release":     {"refs/tags/"},
	"public-sync": {"refs/heads/", "refs/tags/"},
}

// RulesetRefPatterns returns the GitHub ruleset ref patterns that cover exactly the refs a rule's
// ref pattern matches, as full refs. GitHub documents ruleset matching as Ruby File.fnmatch with
// FNM_PATHNAME, where `*` and a bare `**` stop at `/` and only `**/` crosses it. Parse allows `*`
// only as a pattern's last character, meaning "starts with pre"; under fnmatch that is the pair
// pre* (no further /) and pre*/**/* (one or more). testdata/fnmatch.tsv checks the pair against
// Ruby's fnmatch row by row. GitHub's docs don't say whether it folds case: if it doesn't, the
// rulesets cover exactly what Decide covers; if it does, they cover a superset. Either way
// nothing the policy gates is left ungated. `policy gates` emits these, and the executor's
// ruleset check (Task 10e) compares with them.
func RulesetRefPatterns(action, ref string) []string {
	if ref == "" {
		return nil
	}
	var out []string
	for _, pre := range rulesetPrefixes[action] {
		out = append(out, pre+ref)
		if strings.HasSuffix(ref, "*") {
			out = append(out, pre+ref+"/**/*")
		}
	}
	return out
}

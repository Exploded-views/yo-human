// Package policy is the owner's approval policy (decision 1): for each action category and
// target it decides passkey, standing or deny. Standard library only.
package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Mode is what the policy decides for an action on a target.
type Mode string

// The modes.
const (
	Passkey  Mode = "passkey"
	Standing Mode = "standing"
	Deny     Mode = "deny"
)

// Target is what an action acts on.
type Target struct {
	Repo, Ref, Env string
}

// Rule is one line of the policy file. Each pattern holds literal characters and `*`, which
// matches any run of characters, `/` included, so ref `*` is every branch or tag, release/1.2
// too; an empty pattern matches only an empty value. Repo is `owner/name` (matched without
// regard to case); merge, release and public-sync rules name a ref and no env; deploy rules
// name an env and no ref. Refs are matched case-sensitively, as git compares them: a deny on
// `v*` doesn't cover a tag `V1.0`, so a policy that means every tag says `*`.
type Rule struct {
	Action string `json:"action"`
	Repo   string `json:"repo"`
	Ref    string `json:"ref"`
	Env    string `json:"env"`
	Mode   Mode   `json:"mode"`
}

// Policy is the parsed policy file.
type Policy struct {
	V     int    `json:"v"`
	Rules []Rule `json:"rules"`
}

// categories are the actions a rule may name.
var categories = map[string]bool{
	"merge": true, "release": true, "deploy": true, "public-sync": true,
	"force-push": true, "delete": true, "spend": true, "secrets": true, "money": true,
}

// NeverListed reports whether action a is hard-coded deny, whatever the policy says.
func NeverListed(a string) bool {
	switch a {
	case "force-push", "delete", "spend", "secrets", "money":
		return true
	}
	return false
}

// Pattern alphabets: the characters a real target can hold (bus validation), plus `*`.
var (
	repoPatRE = regexp.MustCompile(`^[A-Za-z0-9*][A-Za-z0-9*-]*/[A-Za-z0-9._*-]+$`)
	refPatRE  = regexp.MustCompile(`^[A-Za-z0-9._/*-]+$`)
	envPatRE  = regexp.MustCompile(`^[a-z0-9*-]+$`)
)

// Pattern length caps, the targets' own limits (bus validation).
const (
	maxRepoPat = 200
	maxRefPat  = 100
	maxEnvPat  = 63
)

// keyNames are the only keys a policy file may use. encoding/json matches keys without regard
// to case (and folds runes such as U+017F to s), and the last duplicate wins, so checkKeys
// refuses any key not spelled exactly so, and any key given twice in one object.
var keyNames = map[string]bool{"v": true, "rules": true, "action": true, "repo": true, "ref": true, "env": true, "mode": true}

// Parse reads a policy file: strict JSON, exact and unique keys, version 1, every rule checked.
func Parse(b []byte) (Policy, error) {
	if err := checkKeys(json.NewDecoder(bytes.NewReader(b))); err != nil {
		return Policy{}, fmt.Errorf("policy: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var p Policy
	if err := dec.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("policy: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Policy{}, errors.New("policy: data after the policy object")
	}
	if p.V != 1 {
		return Policy{}, fmt.Errorf("policy: version %d, want 1", p.V)
	}
	for i, r := range p.Rules {
		if err := checkRule(r); err != nil {
			return Policy{}, fmt.Errorf("policy: rule %d: %w", i, err)
		}
	}
	return p, nil
}

// checkKeys reads one JSON value and refuses a key outside keyNames or repeated in its object.
func checkKeys(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	switch t {
	case json.Delim('{'):
		return checkObject(dec)
	case json.Delim('['):
		for dec.More() {
			if err := checkKeys(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	}
	return nil
}

func checkObject(dec *json.Decoder) error {
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		k, _ := t.(string)
		if !keyNames[k] || seen[k] {
			return fmt.Errorf("key %q is unknown, not in canonical case, or repeated", k)
		}
		seen[k] = true
		if err := checkKeys(dec); err != nil {
			return err
		}
	}
	_, err := dec.Token()
	return err
}

func checkRule(r Rule) error {
	switch {
	case !categories[r.Action]:
		return fmt.Errorf("action %q is not a category", r.Action)
	case NeverListed(r.Action):
		return fmt.Errorf("action %q is on the never-list", r.Action)
	case rank(r.Mode) == 0:
		return fmt.Errorf("mode %q is not passkey, standing or deny", r.Mode)
	case !repoPatRE.MatchString(r.Repo) || strings.HasSuffix(strings.ToLower(r.Repo), ".git"):
		return fmt.Errorf("repo %q: an owner/name pattern of letters, digits, ., _, - and *, with no .git alias", r.Repo)
	case len(r.Repo) > maxRepoPat:
		return fmt.Errorf("repo pattern: at most %d bytes", maxRepoPat)
	case r.Action == "deploy":
		if len(r.Env) > maxEnvPat {
			return fmt.Errorf("env pattern: at most %d bytes", maxEnvPat)
		}
		return checkFields(r, "env", r.Env, envPatRE, "ref", r.Ref)
	}
	if err := checkFields(r, "ref", r.Ref, refPatRE, "env", r.Env); err != nil {
		return err
	}
	return checkRefPattern(r.Ref)
}

// checkRefPattern: no refs/ prefix, at most maxRefPat bytes, and `*` only as the last character.
// GitHub rulesets match with fnmatch, where `*` stops at `/`; a trailing `*` is the one form
// whose policy meaning ("starts with") a pair of ruleset patterns covers exactly
// (RulesetRefPatterns). The rule holds for every mode, so no rule matches differently from
// the ruleset it implies.
func checkRefPattern(ref string) error {
	switch {
	case strings.HasPrefix(ref, "refs/"):
		return errors.New("a ref pattern names the branch or tag, without refs/")
	case len(ref) > maxRefPat:
		return fmt.Errorf("ref pattern: at most %d bytes", maxRefPat)
	case strings.Contains(strings.TrimSuffix(ref, "*"), "*"):
		return fmt.Errorf("ref pattern %q: * may appear once, only as the last character, so a GitHub ruleset (where * stops at /) can cover exactly what the rule matches", ref)
	}
	return nil
}

// checkFields requires the field the action matches on (want) to fit its alphabet, and the
// field it never matches on (unused) to be empty, so no rule names a target that can't exist.
func checkFields(r Rule, want, v string, re *regexp.Regexp, unused, u string) error {
	if !re.MatchString(v) {
		return fmt.Errorf("a %s rule needs a %s pattern of %s", r.Action, want, re)
	}
	if u != "" {
		return fmt.Errorf("a %s rule names no %s", r.Action, unused)
	}
	return nil
}

// rank orders the modes for precedence: deny beats passkey beats standing. A mode that isn't
// one of the three (only possible in a hand-built Policy) ranks as deny in Decide.
func rank(m Mode) int {
	switch m {
	case Standing:
		return 1
	case Passkey:
		return 2
	case Deny:
		return 3
	}
	return 0
}

// Decide returns the mode for action a on target t. The never-list is checked first. Then, over
// every rule for a whose patterns all match t, the strongest mode wins, whatever the file
// order. No match is deny.
func (p Policy) Decide(a string, t Target) Mode {
	if NeverListed(a) {
		return Deny
	}
	best := 0
	var mode Mode
	for _, r := range p.Rules {
		if r.Action != a || !r.matches(t) {
			continue
		}
		k := rank(r.Mode)
		if k == 0 {
			return Deny
		}
		if k > best {
			best, mode = k, r.Mode
		}
	}
	if best == 0 {
		return Deny
	}
	return mode
}

func (r Rule) matches(t Target) bool {
	return match(strings.ToLower(r.Repo), strings.ToLower(t.Repo)) && match(r.Ref, t.Ref) && match(r.Env, t.Env)
}

func match(pat, v string) bool {
	if pat == "" {
		return v == ""
	}
	return glob(pat, v)
}

// ErrNoWitness: every rune that could stand for `*` in a witness appears in the policy's
// patterns (only a hand-built Policy can do that), so which rules win can't be computed.
var ErrNoWitness = errors.New("policy: no free witness rune in U+E000-U+F8FF")

// Gates returns the passkey rules that win for at least one target, for the rulesets of
// decision 2.
func (p Policy) Gates() ([]Rule, error) {
	return p.filter(func(r Rule, wins bool) bool { return r.Mode == Passkey && wins })
}

// Shadowed returns the rules that never win, which `policy gates` prints as warnings. With no
// witness it returns ErrNoWitness rather than guess: a rule wrongly called shadowed could be
// deleted while live.
func (p Policy) Shadowed() ([]Rule, error) {
	return p.filter(func(_ Rule, wins bool) bool { return !wins })
}

func (p Policy) filter(keep func(r Rule, wins bool) bool) ([]Rule, error) {
	z, ok := p.freshRune()
	if !ok {
		return nil, ErrNoWitness
	}
	var out []Rule
	for _, r := range p.Rules {
		if keep(r, p.wins(r, z)) {
			out = append(out, r)
		}
	}
	return out, nil
}

// wins reports whether Decide returns r's mode for some target r matches. The witness is r's
// patterns with each `*` replaced by a rune no pattern in the policy contains. Another rule
// matches the witness exactly when its patterns cover r's, so if Decide gives r's mode there,
// r wins; if not, a stronger rule covers r and r wins nowhere. So Gates and Decide agree over
// all strings. (With `*` matching any run, a fresh rune in the witness can only be matched by
// another pattern's `*`, so the cover argument holds; z must be absent from every pattern.)
//
// Real targets use a smaller alphabet (bus validation), and the witness rune is outside it. So
// a gate can be over-listed: one whose every real target is covered piecemeal by deny rules,
// for example one per possible first character, is still listed. Over-listing gates more and
// errs on the safe side.
func (p Policy) wins(r Rule, z rune) bool {
	w := Target{Repo: witness(r.Repo, z), Ref: witness(r.Ref, z), Env: witness(r.Env, z)}
	return p.Decide(r.Action, w) == r.Mode
}

func witness(pat string, z rune) string {
	return strings.ReplaceAll(pat, "*", string(z))
}

// freshRune returns a rune in the private-use area U+E000-U+F8FF that appears in none of the
// policy's patterns, or false when every one does.
func (p Policy) freshRune() (rune, bool) {
	for z := rune(0xE000); z <= 0xF8FF; z++ {
		used := false
		for _, r := range p.Rules {
			if strings.ContainsRune(r.Repo+r.Ref+r.Env, z) || strings.ContainsRune(strings.ToLower(r.Repo), z) {
				used = true
				break
			}
		}
		if !used {
			return z, true
		}
	}
	return 0, false
}

// TargetFor is the one mapping from an item's fields to the policy target, used by the bus and
// by every executor wrapper. A merge is matched on its base branch, never on the PR. On the bus
// that base branch is the poster's claim, unverified (the bus has no network). The executor's
// merge wrapper (Task 10e) checks it against the PR's real base before it merges, and decides
// with its own pinned copy of the policy.
func TargetFor(action, repo, ref, baseRef, target string) Target {
	switch action {
	case "merge":
		return Target{Repo: repo, Ref: baseRef}
	case "deploy":
		return Target{Repo: repo, Env: target}
	default:
		return Target{Repo: repo, Ref: ref}
	}
}

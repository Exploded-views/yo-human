package bus

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Exploded-views/yo-human/internal/hosted/policy"
)

var (
	nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	flagRE = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	repoRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)
	refRE  = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	shaRE  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	prRE   = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)
)

// validRepo: owner/name, the owner not starting with -, the name not . or .., and no .git
// suffix in any case: GitHub resolves o/r.git as o/r, so the alias would dodge a rule on o/r.
// (Renames and transfers are the executor's to catch: it resolves full_name, Task 10e.)
func validRepo(s string) bool {
	return repoRE.MatchString(s) && !strings.HasSuffix(s, "/.") && !strings.HasSuffix(s, "/..") &&
		!strings.HasSuffix(strings.ToLower(s), ".git")
}

// validRef: a branch or tag name that git check-ref-format accepts and an executor can pass as
// one argument, never mistaken for an option or a path. Within the charset: not HEAD, no refs/
// prefix (the policy's ref patterns don't carry one), no leading -, no .., not ending in . or /,
// and no component that is empty, starts with . or ends in .lock.
func validRef(s string) bool {
	return refRE.MatchString(s) && s != "HEAD" && !strings.HasPrefix(s, "refs/") &&
		!strings.HasPrefix(s, "-") && !strings.Contains(s, "..") && !strings.HasSuffix(s, ".") &&
		validRefComponents(s)
}

func validRefComponents(s string) bool {
	for _, c := range strings.Split(s, "/") {
		if c == "" || strings.HasPrefix(c, ".") || strings.HasSuffix(c, ".lock") {
			return false
		}
	}
	return true
}

func validSHA(s string) bool { return shaRE.MatchString(s) }

var (
	kinds      = []Kind{KindApproval, KindQuestion, KindBlocker, KindFinished, KindFailed, KindNotice}
	severities = []Severity{SeverityLow, SeverityNormal, SeverityHigh, SeverityCritical}
	urgencies  = []Urgency{UrgencyPage, UrgencyInterruptWhenGood, UrgencyDigest}
	actions    = []Action{ActionNone, ActionMerge, ActionRelease, ActionDeploy, ActionPublicSync,
		ActionForcePush, ActionDelete, ActionSpend, ActionSecrets, ActionMoney, ActionOther}
)

// ValidSwarm reports whether s may name a swarm: a valid name that is not "duet", where Duet's
// items for the owner live, or "human", which addresses the owner. Node tokens and Duet's
// crew addresses use it; ValidName alone would let a node claim Duet's private swarm.
func ValidSwarm(s string) bool { return ValidName(s) && s != "duet" && s != "human" }

// ValidName reports whether s is a valid swarm or node name.
func ValidName(s string) bool { return nameRE.MatchString(s) }

// Validate checks d against the limits and returns an invalid error naming the first bad
// field. It never changes d: text that reads like instructions is data and is kept verbatim.
// It checks shape only and doesn't decide approvability: posting an approval (Task 5's Post)
// must call both Validate and Limits.Approvable on d.PolicyTarget().
func (d *Draft) Validate(l Limits) error {
	for _, check := range []func(Limits) *Error{d.checkEnums, d.checkText, d.checkLink, d.checkFlags, d.checkRefs, d.checkAction, d.checkActionShape} {
		if e := check(l); e != nil {
			return e
		}
	}
	return nil
}

func (d *Draft) checkEnums(Limits) *Error {
	switch {
	case !slices.Contains(kinds, d.Kind):
		return invalid("kind", "approval, question, blocker, finished, failed or notice")
	case !slices.Contains(severities, d.Severity):
		return invalid("severity", "low, normal, high or critical")
	case !slices.Contains(actions, d.Action):
		return invalid("action", "none, other or an action category")
	case !slices.Contains(urgencies, d.Urgency):
		return invalid("urgency", "page, interrupt-when-good or digest")
	case d.To != "human" && !ValidName(d.To):
		return invalid("to", `"human" or a swarm name`)
	}
	return nil
}

// textField is one free-text field: its name, value, minimum and maximum length, and whether
// the maximum counts bytes instead of characters.
type textField struct {
	name, v  string
	min, max int
	bytes    bool
}

func (d *Draft) checkText(Limits) *Error {
	approval := 0
	if d.Kind == KindApproval {
		approval = 1
	}
	for _, f := range []textField{
		{"title", d.Title, 1, MaxTitle, false},
		{"handle", d.Handle, approval, MaxHandle, false},
		{"summary", d.Summary, 0, MaxSummary, false},
		{"detail", d.Detail, 0, MaxDetail, true},
		{"required_action", d.RequiredAction, 0, MaxRequiredAction, false},
		{"target", d.Target, approval, MaxTarget, false},
	} {
		if e := f.check(); e != nil {
			return e
		}
	}
	return nil
}

func (f textField) check() *Error {
	if !utf8.ValidString(f.v) || strings.ContainsFunc(f.v, isRefusedRune) {
		return invalid(f.name, "valid UTF-8 with no control, format or separator characters (newline, tab, ZWNJ and ZWJ are allowed)")
	}
	n := utf8.RuneCountInString(f.v)
	if f.bytes {
		n = len(f.v)
	}
	if n < f.min || n > f.max {
		return invalid(f.name, "required for an approval, and within the length limit")
	}
	return nil
}

// isRefusedRune: the runes that can make the text the owner reads differ from the text
// stored. Control characters other than newline and tab; the default-ignorable code points,
// which render as nothing: every format character (Cf: bidi marks, embeddings, overrides and
// isolates, tag characters, BOM, zero-width space, word joiner, soft hyphen, U+180E, U+061C),
// Other_Default_Ignorable_Code_Point (U+3164 and U+115F, the Hangul fillers; U+034F, the
// combining grapheme joiner) and the variation selectors, except ZWNJ and ZWJ, which scripts and
// emoji need, and U+FE0F, the emoji presentation selector; the line and paragraph separators
// (Zl, Zp); and U+2800, the blank braille pattern, which renders as a space. The variation
// selector refusal also catches the Mongolian free variation selectors U+180B-180D and U+180F:
// an accepted casualty (Mongolian text renders without them, with default glyph forms).
func isRefusedRune(r rune) bool {
	switch {
	case r == '\n' || r == '\t' || r == 0x200C || r == 0x200D || r == 0xFE0F:
		return false
	case unicode.IsControl(r), unicode.Is(unicode.Cf, r), unicode.In(r, unicode.Zl, unicode.Zp),
		unicode.In(r, unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector), r == 0x2800:
		return true
	}
	return false
}

func (d *Draft) checkLink(l Limits) *Error {
	if d.Link == "" {
		return nil
	}
	const valid = "an https link of at most 2048 bytes, with no userinfo, on an allow-listed host, in valid UTF-8 with no control, format or separator characters"
	if !utf8.ValidString(d.Link) || strings.ContainsFunc(d.Link, isRefusedRune) {
		return invalid("link", valid)
	}
	u, err := url.Parse(d.Link)
	if len(d.Link) > MaxLink || err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || !hostAllowed(u, l) {
		return invalid("link", valid)
	}
	return nil
}

func hostAllowed(u *url.URL, l Limits) bool {
	host := strings.ToLower(u.Hostname())
	for _, h := range l.LinkHosts {
		h = strings.ToLower(h)
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

func (d *Draft) checkFlags(Limits) *Error {
	if len(d.RiskFlags) > MaxRiskFlags {
		return invalid("risk_flags", "at most 8 flags")
	}
	for _, f := range d.RiskFlags {
		if !flagRE.MatchString(f) {
			return invalid("risk_flags", "each flag matching ^[a-z0-9-]{1,40}$")
		}
	}
	return nil
}

// refField is one identifier field: empty, or valid within max bytes.
type refField struct {
	name, v, valid string
	ok             func(string) bool
	max            int
}

func (d *Draft) checkRefs(Limits) *Error {
	for _, f := range []refField{
		{"repo", d.Repo, "owner/name", validRepo, 200},
		{"ref", d.Ref, "a PR number or a tag, at most 100 characters", validRef, MaxRef},
		{"base_ref", d.BaseRef, "a branch name, at most 100 characters", validRef, MaxRef},
		{"pinned_commit", d.PinnedCommit, "a full 40-hex lowercase SHA", validSHA, 40},
		{"pinned_tree", d.PinnedTree, "a full 40-hex lowercase SHA", validSHA, 40},
	} {
		if f.v != "" && (len(f.v) > f.max || !f.ok(f.v)) {
			return invalid(f.name, f.valid)
		}
	}
	return nil
}

// required lists, per action, the fields an approval must set, in the order they are checked.
var required = map[Action][]string{
	ActionMerge:      {"repo", "ref", "base_ref", "pinned_commit"},
	ActionRelease:    {"repo", "ref", "pinned_commit"},
	ActionDeploy:     {"repo", "target", "pinned_commit"},
	ActionPublicSync: {"repo", "ref", "pinned_commit", "pinned_tree"},
}

func (d *Draft) checkAction(Limits) *Error {
	if d.Action != ActionNone && d.Kind != KindApproval {
		return invalid("action", "none unless the item is an approval")
	}
	fields := map[string]string{"repo": d.Repo, "ref": d.Ref, "base_ref": d.BaseRef,
		"pinned_commit": d.PinnedCommit, "pinned_tree": d.PinnedTree, "target": d.Target}
	if d.Action == ActionOther {
		for _, f := range []string{"repo", "ref", "base_ref", "pinned_commit", "pinned_tree"} {
			if fields[f] != "" {
				return invalid(f, "empty: an other approval authorizes nothing the executor runs")
			}
		}
	}
	for _, f := range required[d.Action] {
		if fields[f] == "" {
			return invalid(f, "required for a "+string(d.Action)+" approval")
		}
	}
	return nil
}

// checkActionShape: a merge's ref is the PR number, and a deploy's target is the environment,
// in the alphabet the policy's env patterns use, so "Prod" or "prod\t" can't slip past a rule.
func (d *Draft) checkActionShape(Limits) *Error {
	if d.Action == ActionMerge && !prRE.MatchString(d.Ref) {
		return invalid("ref", "the PR number")
	}
	if d.Action == ActionDeploy && !ValidName(d.Target) {
		return invalid("target", "the environment: ^[a-z0-9][a-z0-9-]{0,62}$")
	}
	return nil
}

// PolicyTarget is the policy target of d, through the one mapping policy.TargetFor. For a merge
// that is d.BaseRef, the poster's claim, which the bus can't verify; the executor's merge
// wrapper (Task 10e) checks it against the PR's real base. A walled codename, cn:<name>, is
// looked up in the policy as cn/<name>, so a post and any later re-check of the stored item gate
// on the same target.
func (d *Draft) PolicyTarget() policy.Target {
	return policy.TargetFor(string(d.Action), codenameAsSlug(d.Repo), d.Ref, d.BaseRef, d.Target)
}

// codenameAsSlug is a walled codename, cn:<name>, as the owner/name shape the policy and
// Validate know: cn/<name>. Anything else is returned as is.
func codenameAsSlug(repo string) string {
	if wallCodenameRE.MatchString(repo) {
		return "cn/" + strings.TrimPrefix(repo, "cn:")
	}
	return repo
}

// Approvable returns nil only when action a on target t may be posted as an approval: the
// policy says passkey, or a is other (which authorizes nothing the executor runs). A standing
// target gets code standing (proceed under standing authority; no challenge, no push), and a
// deny target gets excluded_action.
func (l Limits) Approvable(a Action, t policy.Target) error {
	switch {
	case a == ActionNone:
		return &Error{Code: CodeNotApprovable, What: "action", Valid: "an action category or other", Fix: "post a question instead"}
	case a == ActionOther:
		return nil
	case !slices.Contains(actions, a):
		return invalid("action", "none, other or an action category")
	}
	switch l.Policy.Decide(string(a), t) {
	case policy.Passkey:
		return nil
	case policy.Standing:
		return &Error{Code: CodeStanding, What: "action", Valid: "a passkey target", Fix: "no approval is needed: proceed under standing authority"}
	default:
		return &Error{Code: CodeExcludedAction, What: "action", Valid: "a passkey target in the owner's policy", Fix: "do not proceed; the owner's policy excludes this action here"}
	}
}

// asValidated is a stub for the RED commit.
func asValidated(d Draft) Draft { return d }

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
	repoRE = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	refRE  = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	shaRE  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

var (
	kinds      = []Kind{KindApproval, KindQuestion, KindBlocker, KindFinished, KindFailed, KindNotice}
	severities = []Severity{SeverityLow, SeverityNormal, SeverityHigh, SeverityCritical}
	actions    = []Action{ActionNone, ActionMerge, ActionRelease, ActionDeploy, ActionPublicSync,
		ActionForcePush, ActionDelete, ActionSpend, ActionSecrets, ActionMoney, ActionOther}
)

// ValidName reports whether s is a valid swarm or node name.
func ValidName(s string) bool { return nameRE.MatchString(s) }

// Validate checks d against the limits and returns an invalid error naming the first bad
// field. It never changes d: text that reads like instructions is data and is kept verbatim.
func (d *Draft) Validate(l Limits) error {
	for _, check := range []func(Limits) *Error{d.checkEnums, d.checkText, d.checkLink, d.checkFlags, d.checkRefs, d.checkAction} {
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
	if !utf8.ValidString(f.v) || strings.ContainsFunc(f.v, isRefusedControl) {
		return invalid(f.name, "valid UTF-8 with no control characters other than newline and tab")
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

func isRefusedControl(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }

func (d *Draft) checkLink(l Limits) *Error {
	if d.Link == "" {
		return nil
	}
	const valid = "an https link of at most 2048 bytes, with no userinfo, on an allow-listed host"
	u, err := url.Parse(d.Link)
	if len(d.Link) > MaxLink || err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" {
		return invalid("link", valid)
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range l.LinkHosts {
		h = strings.ToLower(h)
		if host == h || strings.HasSuffix(host, "."+h) {
			return nil
		}
	}
	return invalid("link", valid)
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

// refField is one identifier field: empty, or matching re within max bytes.
type refField struct {
	name, v, valid string
	re             *regexp.Regexp
	max            int
}

func (d *Draft) checkRefs(Limits) *Error {
	for _, f := range []refField{
		{"repo", d.Repo, "owner/name", repoRE, 200},
		{"ref", d.Ref, "a PR number or a tag, at most 100 characters", refRE, MaxRef},
		{"base_ref", d.BaseRef, "a branch name, at most 100 characters", refRE, MaxRef},
		{"pinned_commit", d.PinnedCommit, "a full 40-hex lowercase SHA", shaRE, 40},
		{"pinned_tree", d.PinnedTree, "a full 40-hex lowercase SHA", shaRE, 40},
	} {
		if f.v != "" && (len(f.v) > f.max || !f.re.MatchString(f.v)) {
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

// PolicyTarget is the policy target of d, through the one mapping policy.TargetFor.
func (d *Draft) PolicyTarget() policy.Target {
	return policy.TargetFor(string(d.Action), d.Repo, d.Ref, d.BaseRef, d.Target)
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

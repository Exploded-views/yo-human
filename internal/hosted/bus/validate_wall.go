package bus

import "regexp"

// The client wall (protocol v1.2 section 10): a walled swarm exchanges typed lines only. These
// are the grammars; the plan's "Global Constraints" names them.
var (
	wallHandleRE   = regexp.MustCompile(`^ob-[0-9]+$`)
	wallTitleRE    = regexp.MustCompile(`^ob-[0-9]+ (needs:(owner|none)|ack|done|blocked|ci-red|deploy-ready|prod-down)( sha256:[0-9a-f]{64} bytes:[0-9]+)?$`)
	wallGoHoldRE   = regexp.MustCompile(`^(go|hold) sl:[0-9]+$`)
	wallCodenameRE = regexp.MustCompile(`^cn:[a-z][a-z0-9-]{0,39}$`)
	wallRefRE      = regexp.MustCompile(`^(main|release/[0-9]+(\.[0-9]+)*|v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$`)
	// A target is an environment name or a codename, optionally with a PR number: never a slug.
	wallTargetRE = regexp.MustCompile(`^([a-z0-9][a-z0-9-]{0,62}|cn:[a-z][a-z0-9-]{0,39}(#[0-9]+)?)$`)
)

// CheckWalledDraft returns an invalid error naming the first field of d that is not a typed
// line. It is the rule for what a walled swarm's node posts: the handle is ob-<n> (an empty
// handle is refused, since the item id would replace it), the title is the typed grammar,
// summary, detail, required action and link are empty, a repo is a codename (cn:<name>), refs
// are main, release/<n> or v<semver>, and a target is an environment or a codename. Risk flags
// stay: they are codes. It checks text only; Validate still decides shape and approvability.
func CheckWalledDraft(d Draft) error {
	for _, c := range []struct {
		field, v, valid string
		ok              func(string) bool
	}{
		{"handle", d.Handle, "ob-<n>", wallHandleRE.MatchString},
		{"title", d.Title, "ob-<n> then needs:owner, needs:none, ack, done, blocked, ci-red, deploy-ready or prod-down, optionally then sha256:<64 hex> bytes:<n>", wallTitleRE.MatchString},
		{"summary", d.Summary, "empty", empty},
		{"detail", d.Detail, "empty", empty},
		{"required_action", d.RequiredAction, "empty", empty},
		{"link", d.Link, "empty", empty},
		{"repo", d.Repo, "empty or a codename: cn:<name>", orEmpty(wallCodenameRE.MatchString)},
		{"ref", d.Ref, "empty, main, release/<n> or v<major>.<minor>.<patch>", orEmpty(wallRefRE.MatchString)},
		{"base_ref", d.BaseRef, "empty, main, release/<n> or v<major>.<minor>.<patch>", orEmpty(wallRefRE.MatchString)},
		{"target", d.Target, "empty, an environment name or a codename: cn:<name>", orEmpty(wallTargetRE.MatchString)},
	} {
		if !c.ok(c.v) {
			return wallInvalid(c.field, c.valid)
		}
	}
	return nil
}

// CheckWalledMessage checks a message body for a walled swarm. Into the swarm (intoWalled:
// Duet's posts and replies) the only body is exactly "go sl:<n>" or "hold sl:<n>". From the
// swarm (a walled node's reply) it is the typed title grammar.
func CheckWalledMessage(body string, intoWalled bool) error {
	if intoWalled {
		if !wallGoHoldRE.MatchString(body) {
			return wallInvalid("body", "exactly go sl:<n> or hold sl:<n>")
		}
		return nil
	}
	if !wallTitleRE.MatchString(body) {
		return wallInvalid("body", "ob-<n> then needs:owner, needs:none, ack, done, blocked, ci-red, deploy-ready or prod-down, optionally then sha256:<64 hex> bytes:<n>")
	}
	return nil
}

// checkIntoWalledDraft is the rule for a post into a walled swarm (Duet's): the title is
// exactly go sl:<n> or hold sl:<n>, and nothing else in the draft carries text.
func checkIntoWalledDraft(d Draft) error {
	if !wallGoHoldRE.MatchString(d.Title) {
		return wallInvalid("title", "exactly go sl:<n> or hold sl:<n>")
	}
	for _, c := range []struct{ field, v string }{
		{"handle", d.Handle}, {"summary", d.Summary}, {"detail", d.Detail}, {"required_action", d.RequiredAction},
		{"link", d.Link}, {"repo", d.Repo}, {"ref", d.Ref}, {"base_ref", d.BaseRef}, {"target", d.Target},
		{"pinned_commit", d.PinnedCommit}, {"pinned_tree", d.PinnedTree},
	} {
		if c.v != "" {
			return wallInvalid(c.field, "empty: a walled swarm takes only go sl:<n> or hold sl:<n>")
		}
	}
	return nil
}

func empty(s string) bool { return s == "" }

func orEmpty(ok func(string) bool) func(string) bool {
	return func(s string) bool { return s == "" || ok(s) }
}

// wallInvalid is an invalid error whose Fix points at the wall, not at the poster's wording.
func wallInvalid(field, valid string) *Error {
	return &Error{Code: CodeInvalid, What: field, Valid: valid,
		Fix: "this swarm is behind the client wall: send only the typed form, with the detail kept on its own host"}
}

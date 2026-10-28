// Package policy is the owner's approval policy (decision 1): for each action category and
// target it decides passkey, standing or deny. Standard library only.
package policy

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

// Rule is one line of the policy file.
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

// Parse reads a policy file.
func Parse(b []byte) (Policy, error) { return Policy{}, nil }

// Decide returns the mode for action a on target t.
func (p Policy) Decide(a string, t Target) Mode { return "" }

// Gates returns the passkey rules that win for at least one target.
func (p Policy) Gates() []Rule { return nil }

// Shadowed returns the rules that never win.
func (p Policy) Shadowed() []Rule { return nil }

// TargetFor maps an item's fields to the target the policy matches.
func TargetFor(action, repo, ref, baseRef, target string) Target { return Target{} }

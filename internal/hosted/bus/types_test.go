package bus

import (
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/policy"
)

func mustPolicy(t *testing.T, s string) policy.Policy {
	t.Helper()
	p, err := policy.Parse([]byte(s))
	if err != nil {
		t.Fatalf("policy.Parse: %v", err)
	}
	return p
}

func code(err error) Code {
	if e, ok := AsError(err); ok {
		return e.Code
	}
	return ""
}

func TestPolicyDenyRefused(t *testing.T) {
	l := Limits{}
	for _, a := range []Action{ActionMerge, ActionRelease, ActionDeploy, ActionPublicSync, ActionForcePush, ActionDelete, ActionSpend, ActionSecrets, ActionMoney} {
		if got := code(l.Approvable(a, policy.Target{Repo: "o/r", Ref: "main"})); got != CodeExcludedAction {
			t.Errorf("empty policy, %s: code %q, want excluded_action", a, got)
		}
	}
	l.Policy = mustPolicy(t, `{"v": 1, "rules": [{"action": "merge", "repo": "o/listed", "ref": "main", "mode": "passkey"}]}`)
	if got := code(l.Approvable(ActionMerge, policy.Target{Repo: "o/other", Ref: "main"})); got != CodeExcludedAction {
		t.Errorf("unlisted repo: code %q, want excluded_action", got)
	}
}

func TestOtherApprovableWithoutPolicy(t *testing.T) {
	if err := (Limits{}).Approvable(ActionOther, policy.Target{}); err != nil {
		t.Errorf("other under an empty policy: %v, want nil", err)
	}
}

func TestNoneNeverApprovable(t *testing.T) {
	l := Limits{Policy: policy.Policy{V: 1, Rules: []policy.Rule{{Action: "none", Mode: policy.Passkey}}}}
	if got := code(l.Approvable(ActionNone, policy.Target{})); got != CodeNotApprovable {
		t.Errorf("none: code %q, want not_approvable", got)
	}
	if got := code(l.Approvable("launch", policy.Target{})); got != CodeInvalid {
		t.Errorf("unknown action: code %q, want invalid", got)
	}
}

// The bus side of TestPolicyStandingNoPrompt: Approvable says standing. That nothing is
// stored and no challenge or push is made is asserted where items are posted (Task 5).
func TestPolicyStandingNoPrompt(t *testing.T) {
	l := Limits{Policy: mustPolicy(t, `{"v": 1, "rules": [{"action": "merge", "repo": "o/*", "ref": "main", "mode": "standing"}]}`)}
	if got := code(l.Approvable(ActionMerge, policy.Target{Repo: "o/r", Ref: "main"})); got != CodeStanding {
		t.Errorf("standing target: code %q, want standing", got)
	}
}

func TestPolicyPasskeyApprovable(t *testing.T) {
	l := Limits{Policy: mustPolicy(t, `{"v": 1, "rules": [{"action": "release", "repo": "o/r", "ref": "v*", "mode": "passkey"}]}`)}
	if err := l.Approvable(ActionRelease, policy.Target{Repo: "o/r", Ref: "v1.2.3"}); err != nil {
		t.Errorf("passkey target: %v, want nil", err)
	}
}

func TestDraftTarget(t *testing.T) {
	d := mergeApproval()
	if got, want := d.PolicyTarget(), (policy.Target{Repo: "o/r", Ref: "main"}); got != want {
		t.Errorf("merge PolicyTarget = %+v, want %+v (the base branch, not the PR)", got, want)
	}
}

func TestActorKind(t *testing.T) {
	cases := map[PrincipalKind]ActorKind{
		PrincipalPasskey: ActorHuman, PrincipalDesk: ActorHuman,
		PrincipalDuet: ActorAgent, PrincipalNode: ActorAgent, PrincipalExecutor: ActorAgent,
		PrincipalDeskStart: ActorAgent, "": ActorAgent, "human": ActorAgent,
	}
	for k, want := range cases {
		if got := (Principal{Kind: k}).ActorKind(); got != want {
			t.Errorf("ActorKind(%q) = %q, want %q", k, got, want)
		}
	}
}

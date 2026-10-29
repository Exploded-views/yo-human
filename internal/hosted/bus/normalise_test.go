package bus

import (
	"reflect"
	"testing"
)

// The one normalisation Validate and the policy see: a walled codename as cn/<name>, and a walled
// merge's PR id as the bare number. A PR id on any other action is not touched (the wall refuses it).
func TestAsValidatedMapsCodenameAndMergePRID(t *testing.T) {
	m := asValidated(Draft{Action: ActionMerge, Repo: "cn:alpha", Ref: "pr:42", BaseRef: "main"})
	if m.Repo != "cn/alpha" || m.Ref != "42" || m.BaseRef != "main" {
		t.Errorf("merge = %+v", m)
	}
	r := asValidated(Draft{Action: ActionRelease, Repo: "o/r", Ref: "pr:42"})
	if r.Repo != "o/r" || r.Ref != "pr:42" {
		t.Errorf("release = %+v: pr: is stripped for a merge only", r)
	}
}

func TestCheckedViewAndPolicyTargetUseTheSameNormalisation(t *testing.T) {
	b := New(nil, nil, Limits{WalledSwarms: []string{"client"}})
	d := Draft{Action: ActionMerge, Repo: "cn:alpha", Ref: "pr:42", BaseRef: "main"}
	if got, want := b.checkedView("client", d), asValidated(d); !reflect.DeepEqual(got, want) {
		t.Errorf("walled view = %+v, want %+v", got, want)
	}
	if got := b.checkedView("crew", d); !reflect.DeepEqual(got, d) {
		t.Errorf("unwalled view = %+v, want the draft as posted", got)
	}
	n := asValidated(d)
	if got, want := d.PolicyTarget(), n.PolicyTarget(); got != want {
		t.Errorf("PolicyTarget of the draft %+v, of its normalised form %+v", got, want)
	}
}

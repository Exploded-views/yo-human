//go:build e2e

package e2e

import (
	"testing"
)

// Two projects may use the same dedup key without touching each other's incident, and PagerDuty
// sees the key namespaced by project too (fix round 2, M2), so one project cannot fold its page
// into, or resolve, another's alert.
func TestIncidentsAreNamespacedByProjectAtPagerDutyToo(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	sb.extraEnv = []string{"YO_HUMAN_PROJECT=alpha"}
	sb.run("notify", "--level", "urgent", "--title", "Disk full", "--ask", "Free space", "--dedup-key", "disk-7").want(t, 0, "delivered")
	sb.extraEnv = []string{"YO_HUMAN_PROJECT=beta"}
	sb.run("resolve", "--dedup-key", "disk-7").want(t, 0, "noop")
	sb.pd.wantCount(t, 1)
	sb.extraEnv = []string{"YO_HUMAN_PROJECT=alpha"}
	sb.run("resolve", "--dedup-key", "disk-7").want(t, 0, "resolved")
	for i, q := range sb.pd.requests() {
		if ev := q.obj(t); ev["dedup_key"] != "5:alpha/disk-7" {
			t.Errorf("PagerDuty request %d has dedup_key %v, want 5:alpha/disk-7", i, ev["dedup_key"])
		}
	}
	sb.pd.wantCount(t, 2)
}

// With YO_HUMAN_PROJECT set by the harness, an agent cannot reach another project's incident with
// --project, nor list every project's.
func TestAnotherProjectIsOutOfReach(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	sb.extraEnv = []string{"YO_HUMAN_PROJECT=victim"}
	sb.run("notify", "--level", "urgent", "--title", "Disk full", "--ask", "Free space", "--dedup-key", "db-1").want(t, 0, "delivered")
	sb.extraEnv = []string{"YO_HUMAN_PROJECT=attacker"}
	sb.run("resolve", "--project", "victim", "--dedup-key", "db-1").wantExit(t, 64)
	sb.run("status", "--all").wantExit(t, 64)
	sb.pd.wantCount(t, 1)
	sb.extraEnv = []string{"YO_HUMAN_PROJECT=victim"}
	if v := sb.status(); len(v.Incidents) != 1 {
		t.Errorf("victim's incidents after the attempt: %+v", v.Incidents)
	}
}

package main

import (
	"strings"
	"testing"
	"time"
)

func fakeHelper(t *testing.T, script string) {
	t.Helper()
	old := hostedArgv
	hostedArgv = func(string, int) []string { return []string{"sh", "-c", script} }
	t.Cleanup(func() { hostedArgv = old })
}

func TestHostedScenariosAreRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, s := range scenarios {
		have[s.Name] = s.Sample != nil
	}
	for _, name := range []string{"hosted-post", "hosted-revise"} {
		if !have[name] {
			t.Errorf("scenario %s is not registered with a Sample", name)
		}
	}
}

func TestHostedSampleReadsTheHelperOutput(t *testing.T) {
	fakeHelper(t, `printf '1000000\n2500000\n'`)
	got, err := hostedSample("post")(2)
	want := []time.Duration{time.Millisecond, 2500 * time.Microsecond}
	if err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, %v, want %v", got, err, want)
	}
}

func TestHostedSampleRefusesBadOutput(t *testing.T) {
	for name, script := range map[string]string{
		"too few lines": `printf '1000000\n'`, "not a number": `printf '1000000\nfast\n'`,
		"zero": `printf '1000000\n0\n'`, "negative": `printf '1000000\n-5\n'`,
		"helper fails": `echo boom >&2; exit 3`, "too many lines": `printf '1\n2\n3\n'`,
	} {
		fakeHelper(t, script)
		if got, err := hostedSample("post")(2); err == nil {
			t.Errorf("%s: no error, got %v", name, got)
		}
	}
}

func TestHostedSampleNamesTheScenarioInItsError(t *testing.T) {
	fakeHelper(t, `exit 3`)
	if _, err := hostedSample("revise")(1); err == nil || !strings.Contains(err.Error(), "revise") {
		t.Errorf("error = %v, want one that names the scenario", err)
	}
}

func TestHostedArgvRunsTheHelper(t *testing.T) {
	got := strings.Join(hostedArgv("post", 20), " ")
	if got != "go run ./internal/hosted/perf/cmd post 20" {
		t.Errorf("argv = %q", got)
	}
}

func TestBudgetsFileHasTheHostedScenarios(t *testing.T) {
	b, err := ReadBudgets("../../perf-budgets.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hosted-post", "hosted-revise"} {
		bud, ok := b.Budgets[name]
		if !ok {
			t.Errorf("no budget for %s", name)
		} else if bud.P95Ms <= 0 || bud.P95Ms > 20 {
			t.Errorf("%s budget %dms: must be above 0 and at most the plan's 20ms ceiling", name, bud.P95Ms)
		}
	}
}

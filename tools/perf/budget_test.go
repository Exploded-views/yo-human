package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ms100(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func TestP95NearestRank(t *testing.T) {
	order := []int{7, 1, 20, 13, 4, 16, 9, 2, 18, 11, 5, 19, 3, 14, 8, 12, 6, 17, 10, 15}
	var s []time.Duration
	for _, n := range order {
		s = append(s, ms100(n))
	}
	if got := P95(s); got != ms100(19) {
		t.Errorf("p95 of 1..20ms = %v, want 19ms", got)
	}
	if s[0] != ms100(7) {
		t.Error("P95 reordered its input")
	}
	if got := P95([]time.Duration{ms100(5)}); got != ms100(5) {
		t.Errorf("p95 of one sample = %v, want 5ms", got)
	}
}

var b30 = Budgets{Runs: 20, CIHeadroom: 2.0, Budgets: map[string]Budget{"x": {P95Ms: 30}}}

func TestCheckOverBudgetFails(t *testing.T) {
	vs := Check(map[string][]time.Duration{"x": {ms100(31)}}, b30, false)
	if len(vs) != 1 || vs[0].String() != "x: p95 31.0ms > budget 30.0ms" {
		t.Errorf("got %v, want one violation \"x: p95 31.0ms > budget 30.0ms\"", vs)
	}
}

func TestWithinBudgetPasses(t *testing.T) {
	if vs := Check(map[string][]time.Duration{"x": {ms100(30)}}, b30, false); len(vs) != 0 {
		t.Errorf("p95 equal to budget: got %v, want none", vs)
	}
}

func TestHeadroomOnlyInCI(t *testing.T) {
	res := map[string][]time.Duration{"x": {ms100(31)}}
	if vs := Check(res, b30, true); len(vs) != 0 {
		t.Errorf("ci=true, 31ms vs 30ms x2.0: got %v, want none", vs)
	}
	if vs := Check(res, b30, false); len(vs) != 1 {
		t.Errorf("ci=false: got %v, want 1 violation", vs)
	}
	if vs := Check(map[string][]time.Duration{"x": {ms100(61)}}, b30, true); len(vs) != 1 {
		t.Errorf("ci=true, 61ms vs 60ms: got %v, want 1 violation", vs)
	}
}

func TestScenarioWithoutBudgetFails(t *testing.T) {
	vs := Check(map[string][]time.Duration{"x": {1}, "z": {1}}, b30, false)
	if len(vs) != 1 || vs[0].Scenario != "z" || !strings.Contains(vs[0].Message, "no budget") {
		t.Errorf("got %v, want one \"no budget\" violation for z", vs)
	}
}

func TestBudgetWithoutScenarioFails(t *testing.T) {
	b := Budgets{Runs: 20, CIHeadroom: 2.0, Budgets: map[string]Budget{"x": {30}, "y": {30}}}
	vs := Check(map[string][]time.Duration{"x": {1}}, b, false)
	if len(vs) != 1 || vs[0].Scenario != "y" || !strings.Contains(vs[0].Message, "no scenario") {
		t.Errorf("got %v, want one \"no scenario\" violation for y", vs)
	}
}

func writeBudgets(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "perf-budgets.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func runPerf(t *testing.T, body string, scs []Scenario, ci bool) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run([]string{"--budgets", writeBudgets(t, body)}, &out, &errb, scs, ci)
	return code, out.String(), errb.String()
}

const okBudgets = `{"runs": 3, "ci_headroom": 2.0, "budgets": {"%s": {"p95_ms": 5000}}}`

func budgetsFor(name string) string { return strings.Replace(okBudgets, "%s", name, 1) }

func TestNonZeroExitOfScenarioIsError(t *testing.T) {
	scs := []Scenario{{Name: "boom", Argv: []string{"sh", "-c", "exit 3"}}}
	code, _, stderr := runPerf(t, budgetsFor("boom"), scs, false)
	if code != 2 || !strings.Contains(stderr, "boom") {
		t.Errorf("exit %d, stderr %q; want exit 2 naming boom", code, stderr)
	}
}

func TestUnstartableScenarioIsError(t *testing.T) {
	scs := []Scenario{{Name: "gone", Argv: []string{"/nonexistent/yo-human"}}}
	if code, _, stderr := runPerf(t, budgetsFor("gone"), scs, false); code != 2 || !strings.Contains(stderr, "gone") {
		t.Errorf("exit %d, stderr %q; want exit 2 naming gone", code, stderr)
	}
}

func TestRunWithinBudgetExitsZeroAndPrints(t *testing.T) {
	scs := []Scenario{{Name: "ok", Argv: []string{"true"}}}
	code, stdout, _ := runPerf(t, budgetsFor("ok"), scs, false)
	if code != 0 || !strings.Contains(stdout, "ok: p95 ") || !strings.Contains(stdout, "budget 5000ms") {
		t.Errorf("exit %d, stdout %q; want 0 with p95 and budget printed", code, stdout)
	}
}

func TestRunOverBudgetExitsOne(t *testing.T) {
	body := `{"runs": 2, "ci_headroom": 2.0, "budgets": {"slow": {"p95_ms": 1}}}`
	scs := []Scenario{{Name: "slow", Argv: []string{"sleep", "0.05"}}}
	code, stdout, _ := runPerf(t, body, scs, false)
	if code != 1 || !strings.Contains(stdout, "slow: p95 ") || !strings.Contains(stdout, "> budget 1.0ms") {
		t.Errorf("exit %d, stdout %q; want 1 with \"slow: p95 ... > budget 1.0ms\"", code, stdout)
	}
}

func TestBadInputIsUsageError(t *testing.T) {
	for name, body := range map[string]string{
		"not json":   "{",
		"zero runs":  `{"runs": 0, "ci_headroom": 2.0, "budgets": {}}`,
		"headroom<1": `{"runs": 5, "ci_headroom": 0.5, "budgets": {}}`,
	} {
		if code, _, _ := runPerf(t, body, nil, false); code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
	var out, errb bytes.Buffer
	if code := run([]string{"--budgets", "/nonexistent.json"}, &out, &errb, nil, false); code != 2 {
		t.Errorf("missing file: exit %d, want 2", code)
	}
	if code := run([]string{"--bogus"}, &out, &errb, nil, false); code != 2 {
		t.Errorf("bad flag: exit %d, want 2", code)
	}
}

func TestReadBudgetsRejectsNonPositiveBudget(t *testing.T) {
	for _, v := range []string{"0", "-5"} {
		body := `{"runs": 5, "ci_headroom": 2.0, "budgets": {"fine": {"p95_ms": 10}, "bad": {"p95_ms": ` + v + `}}}`
		_, err := ReadBudgets(writeBudgets(t, body))
		if err == nil || !strings.Contains(err.Error(), "bad") {
			t.Errorf("p95_ms %s: err %v, want an error naming bad", v, err)
		}
	}
	if code, _, stderr := runPerf(t, `{"runs": 5, "ci_headroom": 2.0, "budgets": {"bad": {"p95_ms": 0}}}`, nil, false); code != 2 || !strings.Contains(stderr, "bad") {
		t.Errorf("exit %d, stderr %q; want exit 2 naming bad", code, stderr)
	}
}

func base(runs int, headroom float64, p map[string]int) Budgets {
	b := Budgets{Runs: runs, CIHeadroom: headroom, Budgets: map[string]Budget{}}
	for k, v := range p {
		b.Budgets[k] = Budget{P95Ms: v}
	}
	return b
}

func TestCheckBaseViolations(t *testing.T) {
	old := base(20, 2.0, map[string]int{"a": 10, "b": 10})
	cases := []struct {
		name string
		cur  Budgets
		want string
	}{
		{"budget raised", base(20, 2.0, map[string]int{"a": 15, "b": 10}), "a: budget raised 10ms → 15ms"},
		{"headroom raised", base(20, 2.5, map[string]int{"a": 10, "b": 10}), "ci_headroom: raised 2.0 → 2.5"},
		{"runs lowered", base(10, 2.0, map[string]int{"a": 10, "b": 10}), "runs: lowered 20 → 10"},
		{"budget removed", base(20, 2.0, map[string]int{"a": 10}), "b: budget removed"},
	}
	for _, c := range cases {
		vs := CheckBase(c.cur, old)
		if len(vs) != 1 || vs[0].String() != c.want {
			t.Errorf("%s: got %v, want exactly [%s]", c.name, vs, c.want)
		}
	}
}

func TestCheckBaseAllowsTighteningAndGrowth(t *testing.T) {
	old := base(20, 2.0, map[string]int{"a": 10})
	for name, cur := range map[string]Budgets{
		"unchanged":        base(20, 2.0, map[string]int{"a": 10}),
		"budget lowered":   base(20, 2.0, map[string]int{"a": 5}),
		"headroom lowered": base(20, 1.5, map[string]int{"a": 10}),
		"runs raised":      base(30, 2.0, map[string]int{"a": 10}),
		"scenario added":   base(20, 2.0, map[string]int{"a": 10, "n": 999}),
	} {
		if vs := CheckBase(cur, old); len(vs) != 0 {
			t.Errorf("%s: got %v, want none", name, vs)
		}
	}
}

func TestRunBaseBudgets(t *testing.T) {
	scs := []Scenario{{Name: "ok", Argv: []string{"true"}}}
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.json")
	write := func(body string) {
		if err := os.WriteFile(basePath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run1 := func(path string) (int, string, string) {
		var out, errb bytes.Buffer
		code := run([]string{"--budgets", writeBudgets(t, budgetsFor("ok")), "--base-budgets", path}, &out, &errb, scs, false)
		return code, out.String(), errb.String()
	}
	write(`{"runs": 3, "ci_headroom": 2.0, "budgets": {"ok": {"p95_ms": 100}}}`)
	if code, out, _ := run1(basePath); code != 1 || !strings.Contains(out, "ok: budget raised 100ms → 5000ms") {
		t.Errorf("raised: exit %d, stdout %q; want 1 naming the raise", code, out)
	}
	write(`{"runs": 3, "ci_headroom": 2.0, "budgets": {"ok": {"p95_ms": 9000}}}`)
	if code, out, _ := run1(basePath); code != 0 {
		t.Errorf("lowered: exit %d, stdout %q; want 0", code, out)
	}
	if code, _, stderr := run1(filepath.Join(dir, "missing.json")); code != 2 || !strings.Contains(stderr, "missing.json") {
		t.Errorf("unreadable base: exit %d, stderr %q; want 2 naming the file", code, stderr)
	}
	write(`{`)
	if code, _, _ := run1(basePath); code != 2 {
		t.Errorf("malformed base: exit %d, want 2", code)
	}
}

func TestSetupRunsOnceIntoATempDirTheTimedCommandSees(t *testing.T) {
	var dirs []string
	scs := []Scenario{{
		Name: "fixture",
		Argv: []string{"sh", "-c", `test "$(cat "$MARK")" = ready`},
		Env:  []string{"MARK=$DIR/marker"},
		Setup: func(dir string) error {
			dirs = append(dirs, dir)
			return os.WriteFile(filepath.Join(dir, "marker"), []byte("ready"), 0o600)
		},
	}}
	code, stdout, stderr := runPerf(t, budgetsFor("fixture"), scs, false)
	if code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0 (the command saw the fixture)", code, stdout, stderr)
	}
	if len(dirs) != 1 {
		t.Fatalf("Setup ran %d times for 3 runs, want once", len(dirs))
	}
	if _, err := os.Stat(dirs[0]); !os.IsNotExist(err) {
		t.Errorf("temp dir %s still exists after the run (stat err %v)", dirs[0], err)
	}
}

func TestEnvAppliesOnTopOfTheParentEnvironment(t *testing.T) {
	t.Setenv("PERF_PARENT", "inherited")
	scs := []Scenario{{
		Name: "env",
		Argv: []string{"sh", "-c", `test "$PERF_PARENT" = inherited && test "$PERF_SET" = given`},
		Env:  []string{"PERF_SET=given"},
	}}
	if code, stdout, stderr := runPerf(t, budgetsFor("env"), scs, false); code != 0 {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0", code, stdout, stderr)
	}
}

func TestSetupFailureIsAnErrorNamingTheScenario(t *testing.T) {
	scs := []Scenario{{Name: "badsetup", Argv: []string{"true"}, Setup: func(string) error { return errors.New("no fixture") }}}
	code, _, stderr := runPerf(t, budgetsFor("badsetup"), scs, false)
	if code != 2 || !strings.Contains(stderr, "badsetup") || !strings.Contains(stderr, "no fixture") {
		t.Errorf("exit %d, stderr %q; want exit 2 naming badsetup and the cause", code, stderr)
	}
}

func TestNoTempDirIsAnErrorNamingTheScenario(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	scs := []Scenario{{Name: "notmp", Argv: []string{"true"}}}
	if code, _, stderr := runPerf(t, budgetsFor("notmp"), scs, false); code != 2 || !strings.Contains(stderr, "notmp") {
		t.Errorf("exit %d, stderr %q; want exit 2 naming notmp", code, stderr)
	}
}

func levelsScenario(t *testing.T) Scenario {
	t.Helper()
	for _, s := range scenarios {
		if s.Name == "levels" {
			return s
		}
	}
	t.Fatal("no scenario named levels")
	return Scenario{}
}

func TestLevelsScenarioRunsInAFixtureHome(t *testing.T) {
	sc := levelsScenario(t)
	if sc.Setup == nil || sc.Argv[len(sc.Argv)-1] != "levels" {
		t.Fatalf("scenario levels = %+v; want one that runs `yo-human levels` after a Setup", sc)
	}
	env := strings.Join(sc.Env, "\n")
	for _, want := range []string{"HOME=$DIR", "XDG_CONFIG_HOME=$DIR/config", "XDG_STATE_HOME=$DIR/state", "YO_HUMAN_CONFIG="} {
		if !strings.Contains(env, want) {
			t.Errorf("Env %v lacks %s", sc.Env, want)
		}
	}
}

func TestLevelsFixtureHoldsALadderAndNoSecretValues(t *testing.T) {
	dir := t.TempDir()
	if err := levelsScenario(t).Setup(dir); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "config", "yo-human", "config.yaml"))
	if err != nil || !strings.Contains(string(body), "levels:") || strings.Contains(string(body), "FAKE") {
		t.Errorf("fixture config = %q, %v; want a levels ladder with no secret values", body, err)
	}
}

func TestLevelsFixtureFailsWhereItCannotWrite(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := levelsFixture(blocker); err == nil {
		t.Error("levelsFixture under a file succeeded, want an error")
	}
}

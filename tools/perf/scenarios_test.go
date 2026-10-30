package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func scenarioNamed(t *testing.T, name string) Scenario {
	t.Helper()
	for _, s := range scenarios {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no scenario named %s", name)
	return Scenario{}
}

// binary builds the CLI once per test; the build cache makes the later ones quick.
func binary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "yo-human")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/Exploded-views/yo-human/cmd/yo-human").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// runOnce sets the scenario up in a fresh dir and runs its command once with the built binary.
func runOnce(t *testing.T, sc Scenario, bin string) (code int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	if sc.Setup != nil {
		if err := sc.Setup(dir); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	cmd := exec.Command(bin, sc.Argv[1:]...)
	cmd.Env = os.Environ()
	for _, e := range sc.Env {
		cmd.Env = append(cmd.Env, strings.ReplaceAll(e, "$DIR", dir))
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, out.String(), errb.String()
}

func TestExpectedExitCodeIsNotAnError(t *testing.T) {
	scs := []Scenario{{Name: "usage", Argv: []string{"sh", "-c", "exit 64"}, Exit: 64}}
	if code, _, stderr := runPerf(t, budgetsFor("usage"), scs, false); code != 0 {
		t.Errorf("exit %d, stderr %q; want 0 for a command that exits with the expected code", code, stderr)
	}
}

func TestADifferentExitCodeIsStillAnError(t *testing.T) {
	scs := []Scenario{{Name: "usage", Argv: []string{"sh", "-c", "exit 3"}, Exit: 64}}
	if code, _, stderr := runPerf(t, budgetsFor("usage"), scs, false); code != 2 || !strings.Contains(stderr, "usage") {
		t.Errorf("exit %d, stderr %q; want exit 2 naming usage", code, stderr)
	}
	scs = []Scenario{{Name: "clean", Argv: []string{"true"}, Exit: 64}}
	if code, _, _ := runPerf(t, budgetsFor("clean"), scs, false); code != 2 {
		t.Errorf("exit %d; want exit 2 when the command succeeds but a failure was expected", code)
	}
}

func TestEveryFixtureScenarioRunsInAFixtureHome(t *testing.T) {
	for _, name := range []string{"status", "notify-usage-error", "notify-held-local", "notify-suppressed"} {
		sc := scenarioNamed(t, name)
		env := strings.Join(sc.Env, "\n")
		for _, want := range []string{"HOME=$DIR", "XDG_CONFIG_HOME=$DIR/config", "XDG_STATE_HOME=$DIR/state", "YO_HUMAN_CONFIG="} {
			if !strings.Contains(env, want) {
				t.Errorf("%s: Env %v lacks %s", name, sc.Env, want)
			}
		}
		if sc.Argv[0] != "dist/yo-human" {
			t.Errorf("%s: Argv %v does not run dist/yo-human", name, sc.Argv)
		}
	}
}

func TestStatusScenario(t *testing.T) {
	code, out, errs := runOnce(t, scenarioNamed(t, "status"), binary(t))
	if code != 0 || !strings.HasPrefix(out, "RESULT ok\n") || errs != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want RESULT ok", code, out, errs)
	}
}

func TestNotifyUsageErrorScenario(t *testing.T) {
	sc := scenarioNamed(t, "notify-usage-error")
	code, out, errs := runOnce(t, sc, binary(t))
	if sc.Exit != 64 || code != 64 || out != "" || !strings.HasPrefix(errs, "error: ") {
		t.Errorf("Exit %d, exit %d, stdout %q, stderr %q; want a usage error (64) that never reaches the config", sc.Exit, code, out, errs)
	}
}

func TestNotifyHeldLocalScenarioHoldsInTheQueue(t *testing.T) {
	sc := scenarioNamed(t, "notify-held-local")
	dir := t.TempDir()
	if err := sc.Setup(dir); err != nil {
		t.Fatal(err)
	}
	secrets, err := os.ReadFile(filepath.Join(dir, "config", "yo-human", "secrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	fake := regexp.MustCompile(`^[A-Z0-9-]*FAKE[A-Z0-9-]*$`)
	for _, l := range strings.Split(strings.TrimSpace(string(secrets)), "\n") {
		if _, v, ok := strings.Cut(l, ": "); !ok || !fake.MatchString(v) {
			t.Errorf("secret line %q is not a FAKE value", l)
		}
	}
	code, out, errs := runOnce(t, sc, binary(t))
	if code != 0 || !strings.HasPrefix(out, "RESULT held ") || errs != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want RESULT held with no network and no error", code, out, errs)
	}
}

func TestNotifySuppressedScenarioRepeatsAnOpenIncident(t *testing.T) {
	sc := scenarioNamed(t, "notify-suppressed")
	dir := t.TempDir()
	if err := sc.Setup(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "state", "yo-human", "incidents.json"))
	if err != nil {
		t.Fatal(err)
	}
	var incs map[string]struct {
		Open bool
		Rank int
	}
	if json.Unmarshal(b, &incs) != nil || len(incs) != 1 {
		t.Fatalf("incidents.json = %q; want one incident", b)
	}
	code, out, errs := runOnce(t, sc, binary(t))
	if code != 0 || !strings.HasPrefix(out, "RESULT suppressed ") || errs != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want RESULT suppressed", code, out, errs)
	}
}

// block makes path a file, so anything that needs it to be a directory fails.
func block(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// makeDir makes path a directory, so writing a file there fails.
func makeDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestHeldFixtureFailsWhereItCannotWrite(t *testing.T) {
	cfg := func(d string) string { return filepath.Join(d, "config", "yo-human") }
	for name, prepare := range map[string]func(string){
		"no config directory": func(d string) { block(t, filepath.Join(d, "config")) },
		"config.yaml":         func(d string) { makeDir(t, filepath.Join(cfg(d), "config.yaml")) },
		"secrets.yaml":        func(d string) { makeDir(t, filepath.Join(cfg(d), "secrets.yaml")) },
	} {
		dir := t.TempDir()
		prepare(dir)
		if err := heldFixture(dir); err == nil {
			t.Errorf("%s: heldFixture succeeded, want an error", name)
		}
		if err := suppressedFixture(dir); err == nil {
			t.Errorf("%s: suppressedFixture succeeded, want an error", name)
		}
	}
}

func TestSuppressedFixtureFailsWhereItCannotWriteTheIncident(t *testing.T) {
	dir := t.TempDir()
	block(t, filepath.Join(dir, "state"))
	if err := suppressedFixture(dir); err == nil {
		t.Error("with a file where the state directory belongs, suppressedFixture succeeded")
	}
	dir = t.TempDir()
	makeDir(t, filepath.Join(dir, "state", "yo-human", "incidents.json"))
	if err := suppressedFixture(dir); err == nil {
		t.Error("with a directory where incidents.json belongs, suppressedFixture succeeded")
	}
}

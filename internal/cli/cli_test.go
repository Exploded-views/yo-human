package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run([]string{"version"}, &out, &errb)
	if code != ExitOK || out.String() != "yo-human dev none\n" || errb.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}

func TestRunNoArgsIsUsage(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run(nil, &out, &errb)
	if code != ExitUsage || !strings.HasPrefix(errb.String(), "usage: yo-human") || out.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}

// The exit codes are a contract with callers (agents), so pin the literals:
// comparing only against the constants cannot catch a changed value.
func TestExitCodeValues(t *testing.T) {
	got := []int{ExitOK, ExitUsage, ExitFailed, ExitInternal, ExitStore, ExitQueued, ExitConfig}
	want := []int{0, 64, 69, 70, 74, 75, 78}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("exit constant %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestErrorFormat(t *testing.T) {
	r := newSandbox(t).do("", "bogus")
	lines := strings.Split(strings.TrimSpace(r.err), "\n")
	expect(t, r.code == ExitUsage && r.out == "" && len(lines) == 3, "code=%d out=%q err=%q; want three stderr lines and exit 64", r.code, r.out, r.err)
	expect(t, lines[0] == "error: unknown command", "first line = %q", lines[0])
	mustNotContain(t, "stderr", r.err, "bogus") // the word may be a pasted value
	expect(t, strings.HasPrefix(lines[1], "valid: ") && strings.HasPrefix(lines[2], "fix: "), "lines = %q", lines)
	mustContain(t, "valid line", lines[1], "config", "levels", "secret", "version")
}

func TestErrorFormatJSON(t *testing.T) {
	r := newSandbox(t).do("", "bogus", "--json")
	var obj struct {
		Version int `json:"version"`
		Error   struct{ What, Valid, Fix string }
	}
	err := json.Unmarshal([]byte(r.out), &obj)
	expect(t, err == nil && r.code == ExitUsage && r.err == "", "code=%d out=%q err=%q (%v); want the error object on stdout, nothing on stderr, exit 64", r.code, r.out, r.err, err)
	expect(t, obj.Version == 1 && obj.Error.What == "unknown command" && obj.Error.Valid != "" && obj.Error.Fix != "", "object = %+v", obj)
	mustNotContain(t, "stdout", r.out, "bogus")
}

func TestGlobalFlagsComeAnywhereAfterTheCommand(t *testing.T) {
	s := newSandbox(t)
	other := write(t, s.ownConfig("other.yaml"), userConfigBody)
	for _, args := range [][]string{
		{"levels", "--config", other, "--json"},
		{"levels", "--config=" + other, "--json"},
		{"levels", "--json", "--config", other},
	} {
		code, out, errb := s.run("", args...)
		if code != ExitOK || !strings.Contains(out, `"levels":[`) {
			t.Errorf("%v: code=%d out=%q err=%q; want the levels as JSON", args, code, out, errb)
		}
	}
	code, _, errb := s.run("", "levels", "--config")
	if code != ExitUsage || !strings.HasPrefix(errb, "error: --config needs a file") {
		t.Errorf("--config with no file: code=%d err=%q; want exit 64", code, errb)
	}
}

func TestSubcommandsNeedAKnownName(t *testing.T) {
	s := newSandbox(t)
	for _, args := range [][]string{{"config"}, {"config", "frob"}, {"secret"}, {"secret", "frob"}} {
		code, _, errb := s.run("", args...)
		if code != ExitUsage || !strings.HasPrefix(errb, "error: "+args[0]+" needs one of its subcommands") {
			t.Errorf("%v: code=%d err=%q; want exit 64", args, code, errb)
		}
		mustNotContain(t, "stderr", errb, "frob") // the word may be a pasted value
	}
	code, _, errb := s.run("", "levels", "extra")
	if code != ExitUsage || !strings.HasPrefix(errb, "error: levels takes no arguments") {
		t.Errorf("levels extra: code=%d err=%q; want exit 64", code, errb)
	}
}

// The command comes first (fix round 3, R2-M2): a flag before it, or between config, secret or
// channel and its subcommand, exits 64, so a harness's prefix rule such as "yo-human secret" or
// "yo-human config trust" matches every way of running that command.
func TestFlagsBeforeTheCommandAreRefused(t *testing.T) {
	s := newSandbox(t)
	s.withConfig()
	cfg := s.userConfig()
	for _, args := range [][]string{
		{"--json", "secret", "set", "x"},
		{"--config=" + cfg, "config", "trust", "f.yaml"},
		{"--json", "config", "init"},
		{"--config", cfg, "levels"},
		{"--json", "status"},
		{"--json", "notify", "--level", "note", "--title", "t"},
		{"config", "--json", "trust", "f.yaml"},
		{"secret", "--config", cfg, "set", "x"},
		{"channel", "--json", "test", "phone"},
		{"--json"},
	} {
		r := s.do("FAKE-VALUE\n", args...)
		expect(t, r.code == ExitUsage && strings.Contains(r.out+r.err, "after the command"), "%v: %+v; want exit 64, flags after the command", args, r)
	}
	_, err := os.Stat(s.secretsFile())
	expect(t, errors.Is(err, os.ErrNotExist), "a refused call stored a secret: %v", err)
	s.do("", "status", "--json").succeeds2(t, "a flag after the command")
	s.do("", "config", "check", "--config", cfg).succeeds2(t, "--config after the subcommand")
}

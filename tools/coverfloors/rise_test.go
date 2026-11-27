package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ff(floors map[string]Floor, exempt map[string]string) FloorsFile {
	return FloorsFile{Floors: floors, Exempt: exempt}
}

func TestCheckRiseLoweredFails(t *testing.T) {
	vs := CheckRise(ff(map[string]Floor{"a": fl(79.9)}, nil), ff(map[string]Floor{"a": fl(80.0)}, nil))
	only(t, vs, "a", "floor lowered 80.0 → 79.9")
}

func TestCheckRiseRemovedFails(t *testing.T) {
	vs := CheckRise(ff(map[string]Floor{}, nil), ff(map[string]Floor{"a": fl(80.0)}, nil))
	only(t, vs, "a", "floor removed")
}

func TestCheckRiseExemptionAddedFails(t *testing.T) {
	cur := ff(map[string]Floor{"a": fl(80.0)}, map[string]string{"a": "slow"})
	vs := CheckRise(cur, ff(map[string]Floor{"a": fl(80.0)}, nil))
	only(t, vs, "a", "exemption added")
}

func TestCheckRiseEqualRaisedAndNewPass(t *testing.T) {
	cur := ff(map[string]Floor{"a": fl(80.0), "b": fl(50.1), "c": fl(0.0)}, map[string]string{"x": "main"})
	base := ff(map[string]Floor{"a": fl(80.0), "b": fl(50.0)}, map[string]string{"x": "main"})
	if vs := CheckRise(cur, base); len(vs) != 0 {
		t.Fatalf("equal, raised and new floors pass in this mode, got %+v", vs)
	}
}

func TestCheckRiseSortedByPackage(t *testing.T) {
	cur := ff(map[string]Floor{"b": fl(1.0), "a": fl(1.0)}, map[string]string{"c": "slow"})
	base := ff(map[string]Floor{"b": fl(2.0), "a": fl(2.0)}, nil)
	vs := CheckRise(cur, base)
	if len(vs) != 3 || vs[0].Pkg != "a" || vs[1].Pkg != "b" || vs[2].Pkg != "c" {
		t.Fatalf("want a, b, c in order, got %+v", vs)
	}
}

// riseArgs writes a head and a base floors file and returns the --rise-only arguments.
func riseArgs(t *testing.T, head, base string) []string {
	t.Helper()
	dir := t.TempDir()
	hp, bp := filepath.Join(dir, "head.json"), filepath.Join(dir, "base.json")
	for p, body := range map[string]string{hp: head, bp: base} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return []string{"--rise-only", "--base-floors", bp, "--floors", hp}
}

func TestRunRiseOnlyExitCodes(t *testing.T) {
	const base = `{"example.com/a":{"statements":80.0}}`
	for _, tc := range []struct {
		name, head, want string
		code             int
	}{
		{"equal", base, "", 0},
		{"raised", `{"example.com/a":{"statements":80.1}}`, "", 0},
		{"new floor", `{"example.com/a":{"statements":80.0},"example.com/b":{"statements":0.0}}`, "", 0},
		{"lowered", `{"example.com/a":{"statements":79.9}}`, "example.com/a: floor lowered 80.0 → 79.9", 1},
		{"removed", `{}`, "example.com/a: floor removed", 1},
		{"exemption", `{"exempt":{"example.com/a":"slow"},"example.com/a":{"statements":80.0}}`, "example.com/a: exemption added", 1},
	} {
		var out, errb bytes.Buffer
		code := run(riseArgs(t, tc.head, base), &out, &errb)
		if code != tc.code || !strings.Contains(out.String(), tc.want) {
			t.Errorf("%s: exit %d out=%q err=%q, want exit %d containing %q", tc.name, code, out.String(), errb.String(), tc.code, tc.want)
		}
	}
}

func TestRunRiseOnlyUsageErrors(t *testing.T) {
	args := riseArgs(t, `{}`, `{}`)
	for name, a := range map[string][]string{
		"no base floors": {"--rise-only", "--floors", args[4]},
		"with update":    append(append([]string{}, args...), "--update"),
		"base missing":   {"--rise-only", "--base-floors", "/nonexistent/base.json", "--floors", args[4]},
		"floors missing": {"--rise-only", "--base-floors", args[2], "--floors", "/nonexistent/floors.json"},
	} {
		var out, errb bytes.Buffer
		if code := run(a, &out, &errb); code != 2 {
			t.Errorf("%s: exit %d, want 2 (out %q err %q)", name, code, out.String(), errb.String())
		}
	}
}

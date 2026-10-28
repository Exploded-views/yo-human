package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mod = "github.com/Exploded-views/yo-human/"

func fl(v float64) Floor { return Floor{Statements: v} }

func only(t *testing.T, vs []Violation, pkg, want string) {
	t.Helper()
	if len(vs) != 1 || vs[0].Pkg != pkg || !strings.Contains(vs[0].Msg, want) {
		t.Fatalf("want one violation for %s containing %q, got %+v", pkg, want, vs)
	}
}

func TestPackageCoverageWeightsStatements(t *testing.T) {
	f, err := os.Open("testdata/two-files.out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	cov, err := PackageCoverage(f)
	if err != nil {
		t.Fatal(err)
	}
	if cov["example.com/m/a"] != 75.0 {
		t.Errorf("a = %v, want 75.0 (3 of 4 statements; a block counts if any line has count > 0)", cov["example.com/m/a"])
	}
	if b, ok := cov["example.com/m/b"]; !ok || b != 0 {
		t.Errorf("b = %v (present %v), want 0.0 present", b, ok)
	}
}

func TestPackageCoverageRejectsMissingMode(t *testing.T) {
	f, err := os.Open("testdata/no-mode.out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := PackageCoverage(f); err == nil {
		t.Fatal("want an error for a profile without a mode: line")
	}
}

func TestPackageCoverageRejectsMalformedBlock(t *testing.T) {
	_, err := PackageCoverage(strings.NewReader("mode: set\nnot a block\n"))
	if err == nil {
		t.Fatal("want an error for a malformed block line")
	}
}

func TestCheckBelowFloorFails(t *testing.T) {
	vs := Check(map[string]float64{"a": 74.9}, []string{"a"}, map[string]Floor{"a": fl(75.0)}, nil)
	only(t, vs, "a", "74.9 < floor 75.0")
}

func TestCheckAtFloorPasses(t *testing.T) {
	vs := Check(map[string]float64{"a": 75.0}, []string{"a"}, map[string]Floor{"a": fl(75.0)}, map[string]Floor{"a": fl(75.0)})
	if len(vs) != 0 {
		t.Fatalf("want none, got %+v", vs)
	}
}

func TestCheckFloorLoweredFails(t *testing.T) {
	vs := Check(map[string]float64{"a": 90}, []string{"a"}, map[string]Floor{"a": fl(75.0)}, map[string]Floor{"a": fl(80.0)})
	only(t, vs, "a", "floor lowered 80.0 → 75.0")
}

func TestCheckFloorRemovedFails(t *testing.T) {
	vs := Check(map[string]float64{}, nil, map[string]Floor{}, map[string]Floor{"a": fl(80.0)})
	only(t, vs, "a", "floor removed")
}

func TestCheckPackageWithoutCoverageFails(t *testing.T) {
	vs := Check(map[string]float64{"a": 80}, []string{"a", "b"}, map[string]Floor{"a": fl(80), "b": fl(50)}, nil)
	only(t, vs, "b", "no coverage (no tests?)")
}

func TestCheckPackageWithoutFloorFails(t *testing.T) {
	vs := Check(map[string]float64{"c": 50}, []string{"c"}, map[string]Floor{}, nil)
	only(t, vs, "c", "no floor; run make floors")
}

func TestUpdateRoundsDown(t *testing.T) {
	if got := RoundDown(75.06); got != 75.0 {
		t.Errorf("75.06 → %v", got)
	}
	if got := RoundDown(75.0); got != 75.0 {
		t.Errorf("75.0 → %v", got)
	}
	if got := RoundDown(100.0 * 3 / 5); got != 60.0 {
		t.Errorf("3/5 → %v, want 60.0", got)
	}
}

func TestUpdateNeverLowers(t *testing.T) {
	got := Update(map[string]float64{"a": 70.04, "b": 91.27}, []string{"a", "b"}, map[string]Floor{"a": fl(80.0)})
	if got["a"].Statements != 80.0 || got["b"].Statements != 91.2 {
		t.Fatalf("got %+v, want a=80.0 (kept) b=91.2", got)
	}
}

func TestExemptPackageNeedsNoFloorOrCoverage(t *testing.T) {
	cmd := mod + "cmd/yo-human"
	cov, pkgs, vs := ApplyExemptions(map[string]string{cmd: "one-line main"},
		map[string]float64{cmd: 0, "a": 80}, []string{cmd, "a"})
	if len(vs) != 0 || len(pkgs) != 1 || pkgs[0] != "a" {
		t.Fatalf("pkgs=%v vs=%+v", pkgs, vs)
	}
	if _, ok := cov[cmd]; ok {
		t.Fatal("exempt package still in coverage map")
	}
	if v := Check(cov, pkgs, map[string]Floor{"a": fl(80)}, nil); len(v) != 0 {
		t.Fatalf("want clean, got %+v", v)
	}
}

func TestExemptionForInternalPackageFails(t *testing.T) {
	p := mod + "internal/cli"
	_, _, vs := ApplyExemptions(map[string]string{p: "hard to test"}, map[string]float64{p: 10}, []string{p})
	only(t, vs, p, "internal")
}

func TestExemptionNeedsReason(t *testing.T) {
	p := mod + "cmd/yo-human"
	_, _, vs := ApplyExemptions(map[string]string{p: "  "}, nil, []string{p})
	only(t, vs, p, "reason")
}

func TestFloorsRoundTripKeepsExempt(t *testing.T) {
	in := `{"exempt":{"x/cmd":"main"},"x/a":{"statements":75.0}}`
	f, err := ParseFloors(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if f.Floors["x/a"].Statements != 75.0 || f.Exempt["x/cmd"] != "main" || len(f.Floors) != 1 {
		t.Fatalf("parsed %+v", f)
	}
	var buf bytes.Buffer
	if err := EncodeFloors(&buf, f); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"statements": 75.0`) || !strings.Contains(buf.String(), `"x/cmd": "main"`) {
		t.Fatalf("encoded %s", buf.String())
	}
}

func TestMissingProfileIsExit2(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"--profile", "/nonexistent/cover.out", "--packages", "/nonexistent/pkgs.txt"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit %d, want 2 (stderr %q)", code, errb.String())
	}
}

func TestNoFlagsIsExit2(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

// setup writes a profile, package list and floors file into a temp dir.
func setup(t *testing.T, floors string) (dir string, args []string) {
	t.Helper()
	dir = t.TempDir()
	w := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	prof := w("cover.out", "mode: set\nexample.com/m/a/a.go:1.1,2.2 4 1\nexample.com/m/a/a.go:3.1,4.2 1 0\n")
	pk := w("pkgs.txt", "example.com/m/a\n")
	fp := w("floors.json", floors)
	return dir, []string{"--profile", prof, "--packages", pk, "--floors", fp}
}

func TestRunCleanAndViolationExitCodes(t *testing.T) {
	_, args := setup(t, `{"example.com/m/a":{"statements":80.0}}`)
	var out, errb bytes.Buffer
	if code := run(args, &out, &errb); code != 0 {
		t.Fatalf("clean: exit %d out=%q err=%q", code, out.String(), errb.String())
	}
	_, args = setup(t, `{"example.com/m/a":{"statements":80.1}}`)
	out.Reset()
	if code := run(args, &out, &errb); code != 1 || !strings.Contains(out.String(), "example.com/m/a: 80.0 < floor 80.1") {
		t.Fatalf("below floor: exit %d out=%q", code, out.String())
	}
}

func TestRunBaseFloorsLoweredIsExit1(t *testing.T) {
	dir, args := setup(t, `{"example.com/m/a":{"statements":70.0}}`)
	base := filepath.Join(dir, "base.json")
	if err := os.WriteFile(base, []byte(`{"example.com/m/a":{"statements":75.0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run(append(args, "--base-floors", base), &out, &errb); code != 1 || !strings.Contains(out.String(), "floor lowered 75.0 → 70.0") {
		t.Fatalf("exit %d out=%q", code, out.String())
	}
}

func TestRunUpdateWritesFloors(t *testing.T) {
	_, args := setup(t, `{"exempt":{"example.com/m/cmd":"one-line main"}}`)
	var out, errb bytes.Buffer
	if code := run(append(args, "--update"), &out, &errb); code != 0 {
		t.Fatalf("exit %d err=%q out=%q", code, errb.String(), out.String())
	}
	b, err := os.ReadFile(args[5])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"statements": 80.0`) || !strings.Contains(string(b), "one-line main") {
		t.Fatalf("floors file: %s", b)
	}
}

func TestRunExemptInternalIsExit1(t *testing.T) {
	_, args := setup(t, `{"exempt":{"github.com/Exploded-views/yo-human/internal/x":"nope"},"example.com/m/a":{"statements":80.0}}`)
	var out, errb bytes.Buffer
	if code := run(args, &out, &errb); code != 1 || !strings.Contains(out.String(), "internal") {
		t.Fatalf("exit %d out=%q", code, out.String())
	}
}

func TestCheckNewFloorBelowCurrentFails(t *testing.T) {
	vs := Check(map[string]float64{"a": 85.67}, []string{"a"}, map[string]Floor{"a": fl(0.0)}, map[string]Floor{})
	only(t, vs, "a", "new floor 0.0 outside [85.1, 85.6]")
}

func TestCheckNewFloorAtCurrentPasses(t *testing.T) {
	vs := Check(map[string]float64{"a": 85.67}, []string{"a"}, map[string]Floor{"a": fl(85.6)}, map[string]Floor{})
	if len(vs) != 0 {
		t.Fatalf("want none, got %+v", vs)
	}
}

// A new floor may sit up to 0.5 below current coverage rounded down (CI and a
// local run measure slightly differently), never above current coverage.
func TestCheckNewFloorWindow(t *testing.T) {
	cov := map[string]float64{"a": 85.67} // rounds down to 85.6; window [85.1, 85.67]
	for _, tc := range []struct {
		floor float64
		ok    bool
	}{
		{85.1, true},  // lower edge, inclusive
		{85.6, true},  // equal to current rounded down: the upper edge at one decimal
		{85.0, false}, // just below the window
		{85.7, false}, // above current
	} {
		vs := Check(cov, []string{"a"}, map[string]Floor{"a": fl(tc.floor)}, map[string]Floor{})
		var hit bool
		for _, v := range vs {
			hit = hit || strings.Contains(v.Msg, "new floor")
		}
		if hit == tc.ok {
			t.Errorf("floor %.1f: new-floor violation %v, want %v (all: %+v)", tc.floor, hit, !tc.ok, vs)
		}
	}
}

func TestCheckNewFloorEqualToCurrentPasses(t *testing.T) {
	vs := Check(map[string]float64{"a": 85.6}, []string{"a"}, map[string]Floor{"a": fl(85.6)}, map[string]Floor{})
	if len(vs) != 0 {
		t.Fatalf("upper edge is inclusive: want none, got %+v", vs)
	}
}

// writeBase writes a base floors file next to the setup files.
func writeBase(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "base.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The bypass: exempting a package while keeping its floor hid it from every check.
func TestRunExemptionAddedIsExit1(t *testing.T) {
	dir, args := setup(t, `{"exempt":{"example.com/m/a":"slow"},"example.com/m/a":{"statements":80.0}}`)
	base := writeBase(t, dir, `{"example.com/m/a":{"statements":80.0}}`)
	var out, errb bytes.Buffer
	code := run(append(args, "--base-floors", base), &out, &errb)
	if code != 1 || !strings.Contains(out.String(), "example.com/m/a: exemption added: needs a constitution amendment") {
		t.Fatalf("exit %d out=%q err=%q", code, out.String(), errb.String())
	}
}

func TestRunExemptionInBasePasses(t *testing.T) {
	floors := `{"exempt":{"example.com/m/cmd":"one-line main"},"example.com/m/a":{"statements":80.0}}`
	dir, args := setup(t, floors)
	base := writeBase(t, dir, floors)
	var out, errb bytes.Buffer
	if code := run(append(args, "--base-floors", base), &out, &errb); code != 0 {
		t.Fatalf("exit %d out=%q err=%q", code, out.String(), errb.String())
	}
}

func TestRunNewFloorAtZeroIsExit1(t *testing.T) {
	dir, args := setup(t, `{"example.com/m/a":{"statements":0.0}}`)
	base := writeBase(t, dir, `{}`)
	var out, errb bytes.Buffer
	code := run(append(args, "--base-floors", base), &out, &errb)
	if code != 1 || !strings.Contains(out.String(), "example.com/m/a: new floor 0.0 outside [79.5, 80.0]") {
		t.Fatalf("exit %d out=%q err=%q", code, out.String(), errb.String())
	}
}

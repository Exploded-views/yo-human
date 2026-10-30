package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func withBinary(b Budgets, n int64) Budgets {
	b.BinaryBytes = n
	return b
}

func TestCheckBaseRejectsARaisedOrRemovedBinaryBudget(t *testing.T) {
	old := withBinary(base(20, 2.0, map[string]int{"a": 10}), 1000)
	for name, c := range map[string]struct {
		cur  int64
		want string
	}{
		"raised by a byte": {1001, "binary_bytes: budget raised 1000 → 1001 bytes"},
		"raised a lot":     {9000000, "binary_bytes: budget raised 1000 → 9000000 bytes"},
		"removed":          {0, "binary_bytes: budget removed"},
	} {
		vs := CheckBase(withBinary(base(20, 2.0, map[string]int{"a": 10}), c.cur), old)
		if len(vs) != 1 || vs[0].String() != c.want {
			t.Errorf("%s: got %v, want exactly [%s]", name, vs, c.want)
		}
	}
}

func TestCheckBaseAllowsALowerOrANewBinaryBudget(t *testing.T) {
	for name, c := range map[string]struct{ old, cur int64 }{
		"unchanged":             {1000, 1000},
		"lowered":               {1000, 999},
		"new, no budget before": {0, 1000},
		"none in either":        {0, 0},
	} {
		old := withBinary(base(20, 2.0, map[string]int{"a": 10}), c.old)
		if vs := CheckBase(withBinary(base(20, 2.0, map[string]int{"a": 10}), c.cur), old); len(vs) != 0 {
			t.Errorf("%s: got %v, want none", name, vs)
		}
	}
}

func TestReadBudgetsRejectsANegativeBinaryBudget(t *testing.T) {
	_, err := ReadBudgets(writeBudgets(t, `{"runs": 5, "ci_headroom": 2.0, "budgets": {}, "binary_bytes": -1}`))
	if err == nil || !strings.Contains(err.Error(), "binary_bytes") {
		t.Errorf("err %v, want an error naming binary_bytes", err)
	}
}

// fakeBinary is a file of n bytes standing in for the built binary.
func fakeBinary(t *testing.T, n int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "yo-human")
	if err := os.WriteFile(p, bytes.Repeat([]byte{0}, n), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func runSized(t *testing.T, budgets, bin string, extra ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	args := append([]string{"--budgets", writeBudgets(t, budgets), "--binary", bin}, extra...)
	code := run(args, &out, &errb, []Scenario{{Name: "ok", Argv: []string{"true"}}}, true)
	return code, out.String(), errb.String()
}

const sized = `{"runs": 1, "ci_headroom": 2.0, "budgets": {"ok": {"p95_ms": 5000}}, "binary_bytes": %d}`

func sizedBudgets(n int) string { return strings.Replace(sized, "%d", strconv.Itoa(n), 1) }

func TestRunChecksTheBinarySizeWithoutHeadroom(t *testing.T) {
	bin := fakeBinary(t, 100)
	if code, out, _ := runSized(t, sizedBudgets(100), bin); code != 0 || !strings.Contains(out, "binary: 100 bytes, budget 100 bytes") {
		t.Errorf("at the budget: exit %d, stdout %q; want 0 with the size printed", code, out)
	}
	if code, out, _ := runSized(t, sizedBudgets(99), bin); code != 1 || !strings.Contains(out, "binary: 100 bytes > budget 99 bytes") {
		t.Errorf("a byte over, CI on: exit %d, stdout %q; want 1 naming the size (no headroom)", code, out)
	}
	if code, _, stderr := runSized(t, sizedBudgets(100), filepath.Join(t.TempDir(), "none")); code != 2 || !strings.Contains(stderr, "none") {
		t.Errorf("no binary: exit %d, stderr %q; want 2 naming the path", code, stderr)
	}
}

func TestRunBaseRejectsARaisedBinaryBudget(t *testing.T) {
	bin := fakeBinary(t, 10)
	basePath := writeBudgets(t, sizedBudgets(100))
	if code, out, _ := runSized(t, sizedBudgets(101), bin, "--base-budgets", basePath); code != 1 || !strings.Contains(out, "binary_bytes: budget raised 100 → 101 bytes") {
		t.Errorf("raised over the base: exit %d, stdout %q; want 1 naming the raise", code, out)
	}
	if code, out, _ := runSized(t, sizedBudgets(90), bin, "--base-budgets", basePath); code != 0 {
		t.Errorf("lowered: exit %d, stdout %q; want 0", code, out)
	}
}

// The repository's budgets file holds the binary's budget, so CheckBase guards it from here on.
func TestTheRepositoryBudgetsHoldABinaryBudget(t *testing.T) {
	b, err := ReadBudgets(filepath.Join("..", "..", "perf-budgets.json"))
	if err != nil || b.BinaryBytes <= 0 {
		t.Errorf("perf-budgets.json: binary_bytes %d, err %v; want a budget", b.BinaryBytes, err)
	}
}

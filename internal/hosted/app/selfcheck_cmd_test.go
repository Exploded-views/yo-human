package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

// TestSelfcheckRunsOffline: the command reads no configuration, builds its database in a temp
// directory and removes it, and says ok. app imports no network package (internal-no-network), so
// nothing it does can leave the machine.
func TestSelfcheckRunsOffline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	var out, errb bytes.Buffer
	env := func(k string) string { t.Errorf("selfcheck read the environment variable %s", k); return "" }
	code := Run([]string{"selfcheck"}, env, &out, &errb)
	if code != ExitOK || out.String() != "selfcheck: ok\n" || errb.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0, \"selfcheck: ok\\n\" and nothing", code, out.String(), errb.String())
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("selfcheck left %d entries in its temp directory", len(left))
	}
}

func TestSelfcheckWithoutATempDirIsInternal(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	var out, errb bytes.Buffer
	code := Run([]string{"selfcheck"}, noEnv, &out, &errb)
	if code != ExitInternal || !strings.Contains(errb.String(), "selfcheck") || out.Len() != 0 {
		t.Errorf("exit %d, stdout %q, stderr %q; want 70 naming selfcheck", code, out.String(), errb.String())
	}
}

func TestSelfcheckTakesNoArguments(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Run([]string{"selfcheck", "extra"}, noEnv, &out, &errb); code != ExitUsage || out.Len() != 0 {
		t.Errorf("exit %d, stdout %q; want 64", code, out.String())
	}
}

func TestSelfcheckInOpensOnlyADatabaseItMay(t *testing.T) {
	if err := selfcheckIn(t.TempDir()); err != nil {
		t.Errorf("selfcheckIn in a private directory: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // a directory that is too open, on purpose
		t.Fatal(err)
	}
	if err := selfcheckIn(dir); err == nil {
		t.Error("selfcheckIn opened a database in a directory open to others")
	}
}

// flaky is a store that fails its nth transaction, or serves no events to its nth.
type flaky struct {
	bus.Store
	failAt, noEventsAt, calls int
}

type noEvents struct{ bus.Tx }

func (noEvents) EventsSince(string, int64, int) ([]bus.Event, error) { return nil, nil }

func (f *flaky) Tx(ctx context.Context, fn func(bus.Tx) error) error {
	f.calls++
	switch f.calls {
	case f.failAt:
		return errors.New("injected store failure")
	case f.noEventsAt:
		return f.Store.Tx(ctx, func(x bus.Tx) error { return fn(noEvents{x}) })
	}
	return f.Store.Tx(ctx, fn)
}

func TestExerciseBusStepsEachFailLoudly(t *testing.T) {
	for _, c := range []struct {
		name string
		f    flaky
		want string
	}{
		{"all steps pass", flaky{}, ""},
		{"the node's post fails", flaky{failAt: 1}, "post"},
		{"Duet's record fails", flaky{failAt: 2}, "record"},
		{"the node's read fails", flaky{failAt: 3}, "read"},
		{"the node reads no answer", flaky{noEventsAt: 3}, "answer"},
	} {
		d, err := db.Open(filepath.Join(t.TempDir(), "data", "bus.db"))
		if err != nil {
			t.Fatal(err)
		}
		c.f.Store = d
		err = exerciseBus(context.Background(), newSelfcheckBus(&c.f))
		_ = d.Close()
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: err = %v, want one naming %q", c.name, err, c.want)
		}
	}
}

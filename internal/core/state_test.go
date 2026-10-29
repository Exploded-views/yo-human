package core_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

// openState opens the state the env's store works on.
func openState(t *testing.T, e *env) *core.State {
	t.Helper()
	s, err := core.OpenState(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOpenStateCreatesAPrivateDirAndRefusesAnOpenOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if _, err := core.OpenState(dir); err != nil {
		t.Fatalf("a missing state dir: %v; want it created", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "queue")); err != nil || !fi.IsDir() {
		t.Errorf("queue/ after OpenState: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := core.OpenState(dir); err == nil {
		t.Error("a state dir others can read was opened; want it refused")
	}
}

func TestStateTrustRecordsAPathAndHash(t *testing.T) {
	s := openState(t, newEnv(t, levelsCfg(time.Second, page(retry5, "a")), one))
	if err := s.Trust("/p/.yo-human.yaml", "aa"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path, sha string
		want      bool
	}{{"/p/.yo-human.yaml", "aa", true}, {"/p/.yo-human.yaml", "bb", false}, {"/q/.yo-human.yaml", "aa", false}} {
		if got, err := s.Trusted(c.path, c.sha); err != nil || got != c.want {
			t.Errorf("Trusted(%s, %s) = %v, %v; want %v", c.path, c.sha, got, err, c.want)
		}
	}
}

func TestStateAuditAppendsALogLine(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), one)
	s := openState(t, e)
	for _, ev := range []string{"trust", "secret-set"} {
		if err := s.Audit(map[string]any{"event": ev}); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(e.dir, "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "{\"event\":\"trust\"}\n{\"event\":\"secret-set\"}\n" {
		t.Errorf("log.jsonl = %q; want the two events, one per line", got)
	}
}

func TestStateWithinGivesUpOnAHeldLock(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	s := openState(t, e)
	defer holdLock(t, e)()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got := make(chan error, 1)
	go func() { _, err := s.Within(ctx).Reached(e.cfg, "k", t0); got <- err }()
	var err error
	select {
	case err = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("Reached behind a held lock waited past its bound")
	}
	if !core.IsBusy(err) {
		t.Errorf("Reached behind a held lock = %v; want a busy lock", err)
	}
	if core.IsBusy(errors.New("other")) || core.IsBusy(nil) {
		t.Error("IsBusy reported an error that is not a busy lock")
	}
}

func TestStatePipelineWorksOnTheState(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), one)
	p := openState(t, e).Pipeline(core.Deps{Config: e.cfg, Channels: map[string]channel.Channel{"a": e.fakes["a"]},
		Caps: one, Now: e.clk.Now, NewID: func() string { return "S1" }})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	if _, err := p.Notify(context.Background(), channel.Message{Level: "page", Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if q, _, _ := e.counts(); q != 1 {
		t.Errorf("queued = %d after a failed send through the state's pipeline; want 1 in this state", q)
	}
}

func TestStateDueInstancesNamesEachInstanceOnceSorted(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), one)
	for _, p := range []store.Pending{
		{Key: "X1.b", Data: []byte(`{"instance":"b"}`)},
		{Key: "X2.a", Data: []byte(`{"instance":"a"}`)},
		{Key: "X3.b", Data: []byte(`{"instance":"b"}`)},
		{Key: "X4.c", Data: []byte(`{"instance":"c"}`), DeliverAt: t0.Add(time.Hour)},
	} {
		if err := e.st.Enqueue(p); err != nil {
			t.Fatal(err)
		}
	}
	got, err := openState(t, e).DueInstances(t0)
	if err != nil || strings.Join(got, " ") != "a b" {
		t.Errorf("DueInstances = %v, %v; want a b (c is not due)", got, err)
	}
	s := openState(t, e)
	if err := os.RemoveAll(filepath.Join(e.dir, "queue")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, "queue"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.DueInstances(t0); err == nil {
		t.Errorf("DueInstances over an unreadable queue = %v; want the error", got)
	}
}

func TestStateRecoverRequeuesAStrandedSend(t *testing.T) {
	e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), one)
	strandedEntry(t, e, time.Hour)
	if err := openState(t, e).Recover(e.cfg); err != nil {
		t.Fatal(err)
	}
	if q, in, _ := e.counts(); q != 1 || in != 0 {
		t.Errorf("queued %d, inflight %d after Recover; want the stranded send queued again", q, in)
	}
}

func TestStateCreateFileNeverReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := core.CreateFile(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := core.CreateFile(path, []byte("two")); !errors.Is(err, fs.ErrExist) {
		t.Errorf("a second CreateFile = %v; want fs.ErrExist", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("CreateFile wrote nothing: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "one" || fi.Mode().Perm() != 0o600 {
		t.Errorf("file = %q, mode %v; want \"one\", 0600", b, fi.Mode().Perm())
	}
}

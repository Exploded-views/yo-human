package core_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// strandedEntry leaves a queued send claimed by a process that has since died, as a crash would.
func strandedEntry(t *testing.T, e *env, age time.Duration) {
	t.Helper()
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.notify("page")
	e.fakes["a"].SendFunc = nil
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot start a process to get a dead pid: %v", err)
	}
	from := filepath.Join(e.dir, "queue", "M001.a.json")
	to := filepath.Join(e.dir, "inflight", fmt.Sprintf("M001.a.%d.json", cmd.Process.Pid))
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-age) // file ages are judged by the real clock
	if err := os.Chtimes(to, old, old); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(5 * time.Minute) // past the retry time too
}

func TestNotifyAndFlushRecoverStrandedSendsFirst(t *testing.T) {
	ops := map[string]func(*env) error{
		"flush": func(e *env) error { _, err := e.p.Flush(context.Background()); return err },
		"notify": func(e *env) error {
			_, err := e.p.Notify(context.Background(), channel.Message{Level: "page"})
			return err
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, levelsCfg(time.Second, page(retry5, "a")), map[string]channel.Capabilities{"a": human})
			strandedEntry(t, e, time.Hour)
			before := len(e.fakes["a"].Sent)
			if err := op(e); err != nil {
				t.Fatal(err)
			}
			var gotOld bool
			for _, m := range e.fakes["a"].Sent[before:] {
				gotOld = gotOld || m.ID == "M001"
			}
			if !gotOld {
				t.Errorf("the send stranded by a dead process was not recovered and resent")
			}
		})
	}
}

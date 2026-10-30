package core_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/store"
)

// Seam-free: the test holds the incident lock while the first send returns. Until the sender can
// take the lock and record the reach, its entry must stay in inflight/, or a repeat admitted in
// that gap finds nothing pending and nothing reached, and pages again.
func TestASentEntryStaysPendingUntilItsReachIsRecorded(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	locked, release := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	var once sync.Once
	e.fakes["a"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		once.Do(func() {
			go func() {
				held <- e.st.WithIncidents(func(map[string]*store.Incident) error {
					close(locked)
					<-release
					return nil
				})
			}()
			<-locked
		})
		return channel.Receipt{Instance: "a"}, nil
	}
	done := make(chan core.Result, 1)
	go func() {
		r, err := e.p.Notify(context.Background(), channel.Message{Level: "page", Title: "t", DedupKey: "k"})
		if err != nil {
			t.Errorf("Notify: %v", err)
		}
		done <- r
	}()
	<-locked
	inflight := filepath.Join(e.dir, "inflight")
	for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if ents, _ := os.ReadDir(inflight); len(ents) == 0 {
			t.Error("the sent entry left inflight/ before the incident lock recorded its reach; a repeat in that gap pages again")
			break
		}
	}
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.Status != core.StatusDelivered {
		t.Fatalf("first = %q", r.Status)
	}
}

// With the BeforeLand seam: a repeat admitted between a successful send and the record of its reach
// is suppressed, so the human is paged once.
func TestARepeatBetweenSendAndRecordIsSuppressed(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), one)
	var repeat core.Result
	fired := false
	core.BeforeLand(e.p, func() {
		if !fired {
			fired = true
			repeat = e.say("page", "k", "t")
		}
	})
	if r := e.say("page", "k", "t"); r.Status != core.StatusDelivered {
		t.Fatalf("first = %q", r.Status)
	}
	if !fired || repeat.Status != core.StatusSuppressed || sentCount(e, "a") != 1 {
		t.Fatalf("repeat = %q, sent %d; want suppressed and one page", repeat.Status, sentCount(e, "a"))
	}
}

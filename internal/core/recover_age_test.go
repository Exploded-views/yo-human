package core_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/store"
)

// plantInflight puts an entry in flight under this process's (live) pid, last touched age ago.
func plantInflight(t *testing.T, e *env, key string, age time.Duration) {
	t.Helper()
	if err := e.st.Enqueue(store.Pending{Key: key, Data: []byte(`{}`), NextAttempt: time.Now(), DeliverAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(e.dir, "inflight", key+"."+strconv.Itoa(os.Getpid())+".json")
	if err := os.Rename(filepath.Join(e.dir, "queue", key+".json"), dst); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(dst, at, at); err != nil {
		t.Fatal(err)
	}
}

// A call requeues an entry in flight longer than max(10 x deadline, 10 min), whatever its pid: no
// call lives that long, so the pid is another process's. A younger entry of a live pid is left alone.
func TestACallRequeuesAnEntryInFlightTooLongForAnyCall(t *testing.T) {
	for _, tc := range []struct {
		deadline, age time.Duration
		requeued      bool
	}{
		{time.Second, 11 * time.Minute, true},
		{time.Second, 9 * time.Minute, false},
		{2 * time.Minute, 15 * time.Minute, false},
		{2 * time.Minute, 21 * time.Minute, true},
	} {
		e := newEnv(t, levelsCfg(tc.deadline, config.Level{Name: "page", Retry: retry5, Send: sends("a")}), map[string]channel.Capabilities{"a": human})
		plantInflight(t, e, "01OLD.a", tc.age)
		if _, err := e.p.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		_, in, _ := e.counts()
		if (in == 0) != tc.requeued {
			t.Errorf("deadline %v, in flight %v: requeued = %v, want %v", tc.deadline, tc.age, in == 0, tc.requeued)
		}
	}
}

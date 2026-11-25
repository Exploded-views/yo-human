package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/db"
)

// fakeBark is a Bark server that records the push bodies it gets.
type fakeBark struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newFakeBark(t *testing.T) *fakeBark {
	t.Helper()
	f := &fakeBark{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, r.URL.Path+" "+string(b))
		f.mu.Unlock()
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeBark) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.bodies...)
}

func TestServeSendsTheOutboxThroughBark(t *testing.T) {
	bark := newFakeBark(t)
	env := serveEnv(t, "YH_BARK_URL", bark.URL, "YH_BARK_USER", "BARK-USER-FAKE", "YH_BARK_PASSWORD", "BARK-PASS-FAKE", "YH_BARK_DEVICE_KEY", "DEVICE-KEY-FAKE")
	a := adminEnv{t: t, path: env["YH_DB_PATH"]}
	a.seedDB()
	if code, _, errs := a.run("outbox", "test"); code != ExitOK {
		t.Fatalf("outbox test: %d %s", code, errs)
	}
	r := startServe(t, env)
	deadline := time.Now().Add(5 * time.Second)
	for len(bark.got()) == 0 {
		if time.Now().After(deadline) {
			r.signal(t, syscall.SIGTERM)
			t.Fatalf("Bark got nothing; serve said %q", r.err.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := bark.got()[0]
	for _, want := range []string{"/push ", `"id":"yh-1"`, `"title":"yo-human"`, `"body":"test push"`, `"device_key":"DEVICE-KEY-FAKE"`} {
		if !strings.Contains(got, want) {
			t.Errorf("push %q lacks %q", got, want)
		}
	}
	if code := r.signal(t, syscall.SIGTERM); code != ExitOK {
		t.Errorf("exit %d, want 0: %s", code, r.err.String())
	}
	for _, secret := range []string{"BARK-PASS-FAKE", "DEVICE-KEY-FAKE"} {
		if strings.Contains(r.err.String()+r.out.String(), secret) {
			t.Errorf("serve printed %s", secret)
		}
	}
}

func TestServeWithoutBarkSaysSoAndRuns(t *testing.T) {
	r := startServe(t, serveEnv(t))
	if code := r.signal(t, syscall.SIGTERM); code != ExitOK {
		t.Errorf("exit %d: %s", code, r.err.String())
	}
	if !strings.Contains(r.err.String(), "YH_BARK_URL") {
		t.Errorf("stderr %q does not say no push will be sent", r.err.String())
	}
}

func TestServeRefusesAHalfConfiguredBark(t *testing.T) {
	replaceServe(t, nil) // a refusal comes before the listeners; if it did not, serve would return at once
	for _, kv := range [][]string{{"YH_BARK_URL", "https://bark.example.com"}, {"YH_BARK_URL", "https://bark.example.com", "YH_BARK_USER", "BARK-USER-FAKE", "YH_BARK_PASSWORD", "BARK-PASS-FAKE"},
		{"YH_BARK_USER", "BARK-USER-FAKE", "YH_BARK_PASSWORD", "BARK-PASS-FAKE", "YH_BARK_DEVICE_KEY", "DEVICE-KEY-FAKE"}} {
		var out, errb strings.Builder
		code := Run([]string{"serve"}, envOf(serveEnv(t, kv...)), &out, &errb)
		if code != ExitConfig || !strings.Contains(errb.String(), "YH_BARK") || strings.Contains(errb.String(), "https://bark") {
			t.Errorf("%v: code=%d err=%q, want a config refusal that names the variable and no value", kv, code, errb.String())
		}
		for _, secret := range []string{"BARK-USER-FAKE", "BARK-PASS-FAKE", "DEVICE-KEY-FAKE"} {
			if strings.Contains(errb.String()+out.String(), secret) {
				t.Errorf("%v: serve printed %s", kv, secret)
			}
		}
	}
}

func TestKickerIsSafeBeforeAndAfterTheWorkerExists(t *testing.T) {
	var k kicker
	k.kick() // no worker yet: the bus may commit before serve has built one
	var out strings.Builder
	a := admin{name: "serve", stderr: &out, env: noEnv}
	cfg := Config{NudgeWindow: time.Minute, NudgeMinSeverity: "normal", NudgeLevel: "active"}
	ch, err := barkChannel(Config{BarkURL: "https://bark.example.com", BarkUser: "BARK-USER-FAKE", BarkPassword: "BARK-PASS-FAKE", BarkDeviceKey: "DEVICE-KEY-FAKE"})
	if err != nil || ch == nil {
		t.Fatalf("barkChannel = %v, %v", ch, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d, err := db.Open(filepath.Join(t.TempDir(), "data", "bus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	wait := a.startWorker(ctx, cfg, ch, bus.New(d, now, bus.Limits{}), d, &k, io.Discard)
	k.kick()
	cancel()
	wait()
	k.kick() // a worker whose Run has ended: Kick still never blocks
}

func TestCheckCallsARefusedMessageRefused(t *testing.T) {
	a := newAdmin(t)
	a.seedDB()
	d, err := db.Open(a.path)
	if err != nil {
		t.Fatal(err)
	}
	failed := time.Now().UTC().Add(-time.Minute)
	_ = d.Tx(context.Background(), func(x bus.Tx) error {
		_, err := x.InsertOutbox(bus.OutboxRow{Kind: bus.OutboxTest, Level: "passive", Title: "t", Body: "b", State: bus.OutboxFailed,
			NextAt: failed, Created: failed, FailedAt: failed, LastStatus: 400})
		return err
	})
	_ = d.Close()
	code, out, _ := a.run("check")
	if code != ExitDegraded || !strings.Contains(out, "delivery: refused\n") || !strings.Contains(out, "delivery_status: 400\n") || !strings.Contains(out, "delivery_failed: 1\n") {
		t.Errorf("code=%d out=%q", code, out)
	}
}

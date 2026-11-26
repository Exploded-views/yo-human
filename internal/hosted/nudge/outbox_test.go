package nudge_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/Exploded-views/yo-human/internal/hosted/notify"
)

func TestDuetPostToHumanSendsItsOwnMessage(t *testing.T) {
	r := newRig(t)
	for _, c := range []struct {
		sev   bus.Severity
		level notify.Level
	}{{bus.SeverityHigh, notify.LevelActive}, {bus.SeverityLow, notify.LevelPassive}, {bus.SeverityCritical, notify.LevelActive}} {
		d := question("d-" + string(c.sev))
		d.Title, d.Summary, d.Severity = "Title "+string(c.sev), "Summary "+string(c.sev), c.sev
		r.post(duet, d)
	}
	r.tick()
	got := r.ch.delivered()
	if len(got) != 3 {
		t.Fatalf("delivered %d, want one message for each of Duet's three posts", len(got))
	}
	for i, want := range []struct {
		title, body string
		level       notify.Level
	}{{"Title high", "Summary high", notify.LevelActive}, {"Title low", "Summary low", notify.LevelPassive}, {"Title critical", "Summary critical", notify.LevelActive}} {
		m := got[i]
		if m.Title != want.title || m.Body != want.body || m.Level != want.level || m.Group != "yo-human" || m.ID != idOf(int64(i+1)) {
			t.Errorf("message %d = %+v, want %+v with id %s and group yo-human", i, m, want, idOf(int64(i+1)))
		}
	}
}

func TestOutboxSentNotResent(t *testing.T) {
	r := newRig(t)
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.tick()
	r.tick()
	r.restart()
	r.advance(time.Hour)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Errorf("%d sends, want the one", n)
	}
	if s := r.row(1); s.State != bus.OutboxSent || !s.SentAt.Equal(t0) || s.Attempts != 1 {
		t.Errorf("row = %+v, want sent at t0 after one attempt", s)
	}
}

func TestOutboxUnsentSentAfterRestart(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("a"))
	r.duetSays(it, "hello") // committed, never ticked: the process died before the send
	r.restart()
	r.tick()
	wantBodies(t, r.pushes(), "hello")
}

func TestOutboxCrashAfterSendResendsSameID(t *testing.T) {
	r := newRig(t)
	id := r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.tick()
	// The crash: the phone has it, but sent was never committed.
	pending := r.row(id)
	pending.State, pending.SentAt, pending.Attempts = bus.OutboxPending, time.Time{}, 0
	r.do(func(x bus.Tx) error { return x.UpdateOutbox(pending) })
	r.restart()
	r.advance(time.Second)
	r.tick()
	got := r.ch.delivered()
	if len(got) != 2 || got[0].ID != idOf(id) || got[1].ID != idOf(id) {
		t.Errorf("delivered %+v, want the same id twice", got)
	}
}

func TestOutboxTransientBackoff(t *testing.T) {
	r := newRig(t)
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.ch.script(statusErr(500), errNetwork, statusErr(408))
	step := func(d time.Duration) { r.advance(d); r.tick() }
	r.tick()
	step(29 * time.Second)
	step(time.Second) // +30 s: the second attempt
	step(59 * time.Second)
	step(time.Second) // +60 s after it: the third
	step(119 * time.Second)
	step(time.Second) // +120 s after that: the fourth, which is accepted
	var at []time.Duration
	for _, a := range r.ch.all() {
		at = append(at, a.at.Sub(t0))
	}
	want := []time.Duration{0, 30 * time.Second, 90 * time.Second, 210 * time.Second}
	if len(at) != len(want) {
		t.Fatalf("attempts at %v, want %v", at, want)
	}
	for i := range want {
		if at[i] != want[i] {
			t.Fatalf("attempts at %v, want %v", at, want)
		}
	}
	if s := r.row(1); s.State != bus.OutboxSent || s.Attempts != 4 {
		t.Errorf("row = %+v, want sent after 4 attempts", s)
	}
}

func TestOutboxBackoffIsCappedAtFiveMinutes(t *testing.T) {
	r := newRig(t)
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.ch.setDefault(statusErr(500))
	var gaps []time.Duration
	for range 8 {
		r.tick()
		next := r.row(1).NextAt
		gaps = append(gaps, next.Sub(r.clk.now()))
		r.clk.advance(next.Sub(r.clk.now()))
	}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for i := range want {
		if gaps[i] != want[i] {
			t.Fatalf("waits = %v, want %v", gaps, want)
		}
	}
}

func TestOutboxRetryAfterHonoured(t *testing.T) {
	r := newRig(t)
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.ch.script(notify.WithRetryAfter(statusErr(429), 120*time.Second))
	r.tick()
	r.advance(119 * time.Second)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Fatalf("%d attempts inside the Retry-After", n)
	}
	r.advance(time.Second)
	r.tick()
	if n := len(r.ch.delivered()); n != 1 {
		t.Errorf("%d delivered at 120 s, want 1", n)
	}
}

func TestRetryAfterCappedAtFiveMinutes(t *testing.T) {
	r := newRig(t)
	r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.ch.script(notify.WithRetryAfter(statusErr(429), time.Hour))
	r.tick()
	r.advance(5*time.Minute - time.Second)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Fatalf("%d attempts before 5 min", n)
	}
	r.advance(time.Second)
	r.tick()
	if n := len(r.ch.delivered()); n != 1 {
		t.Errorf("%d delivered at 5 min, want 1: an hour-long Retry-After is capped", n)
	}
}

func TestOutboxPassStopsAtFirstTransient(t *testing.T) {
	r := newRig(t)
	for range 3 {
		r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	}
	r.ch.script(errNetwork)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Errorf("%d attempts, want the pass to stop at the first transient error", n)
	}
	for _, row := range r.rows() {
		if row.ID != 1 && row.Attempts != 0 {
			t.Errorf("row %d was tried: %+v", row.ID, row)
		}
	}
}

func TestOutboxOldestFirstCapped(t *testing.T) {
	r := newRig(t)
	for range 25 {
		r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
		r.advance(time.Millisecond)
	}
	var per []int
	for range 3 {
		before := len(r.ch.all())
		r.tick()
		per = append(per, len(r.ch.all())-before)
	}
	if per[0] != 10 || per[1] != 10 || per[2] != 5 {
		t.Fatalf("sends per tick = %v, want 10, 10, 5", per)
	}
	for i, a := range r.ch.all() {
		if a.m.ID != idOf(int64(i+1)) {
			t.Fatalf("send %d carried %s, want oldest first", i, a.m.ID)
		}
	}
}

func TestOutboxRefusedMessageFails(t *testing.T) {
	r := newRig(t)
	a := r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.ch.script(statusErr(400))
	r.tick()
	if row := r.row(a); row.State != bus.OutboxFailed || row.LastStatus != 400 || !row.FailedAt.Equal(t0) || row.Attempts != 1 {
		t.Fatalf("row = %+v, want failed at t0 with status 400", row)
	}
	r.advance(time.Minute)
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Fatalf("%d attempts, want a refused message not retried by later ticks", n)
	}
}

func TestRefusedMessageGetsOneMoreTryAfterARestartIfYoung(t *testing.T) {
	r := newRig(t)
	old := r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.ch.script(statusErr(400))
	r.tick() // fails at t0
	r.advance(20 * time.Minute)
	young := r.queue(bus.OutboxTest, bus.Item{}, "yo-human", "test push")
	r.ch.script(statusErr(400))
	r.tick()                    // fails at +20 min
	r.advance(20 * time.Minute) // old is 40 minutes old, young is 20
	before := len(r.ch.all())
	r.ch.script(statusErr(400))
	r.restart()
	r.tick()
	after := r.ch.all()
	if len(after) > before {
		after = after[before:]
	}
	if len(after) != 1 || after[0].m.ID != idOf(young) {
		t.Fatalf("attempts after the restart = %+v, want one, for row %d and not %d", after, young, old)
	}
	if row := r.row(young); row.State != bus.OutboxFailed || !row.Retried {
		t.Errorf("row = %+v, want failed again with retried set", row)
	}
	r.restart()
	r.advance(time.Minute)
	r.tick()
	if n := len(r.ch.all()); n != before+1 {
		t.Errorf("a second restart retried: %d attempts, want %d", n, before+1)
	}
}

func TestRetriedPushRecordedOnceInLedger(t *testing.T) {
	r := newRig(t)
	it := r.post(crewNode, quietItem("a"))
	r.duetSays(it, "hello")
	r.ch.script(statusErr(500))
	r.tick()
	if n := len(r.ledger(it.ID)); n != 0 {
		t.Fatalf("%d ledger rows after a failed send", n)
	}
	r.advance(30 * time.Second)
	r.tick()
	r.advance(time.Hour)
	r.tick()
	ps := r.ledger(it.ID)
	if len(ps) != 1 || ps[0].Source != bus.PushOutbox || ps[0].SourceID != 1 || ps[0].Revision != 1 || !ps[0].At.Equal(t0.Add(30*time.Second)) {
		t.Errorf("ledger = %+v, want exactly one row, written with sent", ps)
	}
}

func TestOutboxLogCarriesIdsOnly(t *testing.T) {
	r := newRig(t)
	d := quietItem("HANDLE-SECRET-FAKE")
	d.Title = "TITLE-SECRET-FAKE"
	it := r.post(crewNode, d)
	r.duetSays(it, "BODY-SECRET-FAKE")
	r.ch.script(notify.WithStatus(errors.New("REASON-SECRET-FAKE DEVICE-KEY-FAKE"), 500))
	r.tick()
	r.advance(time.Minute)
	r.tick()
	logs := r.logs.String()
	for _, s := range []string{"REASON-SECRET", "DEVICE-KEY", "TITLE-SECRET", "BODY-SECRET", "HANDLE-SECRET", "yo-human\""} {
		if strings.Contains(logs, s) {
			t.Errorf("the log carries %q:\n%s", s, logs)
		}
	}
	for _, want := range []string{"outbox_id=1", "item_id=" + it.ID, "kind=duet_post", "status=500", "attempts=1", "attempts=2"} {
		if !strings.Contains(logs, want) {
			t.Errorf("the log lacks %q:\n%s", want, logs)
		}
	}
}

func TestDuetPostSurvivesRestart(t *testing.T) {
	r := newRig(t)
	r.post(duet, question("d1"))
	r.restart()
	r.tick()
	wantBodies(t, r.pushes(), "A summary")
	r.restart()
	r.tick()
	if n := len(r.ch.all()); n != 1 {
		t.Errorf("%d sends after a second restart", n)
	}
}

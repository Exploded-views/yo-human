package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// queuedPage leaves a page queued for retry on both channels, due again in two minutes.
func queuedPage(t *testing.T) (*sandbox, fakeSet) {
	t.Helper()
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	failing(fk["phone"], channel.Transient)
	if r := s.notify("page"); r.code != ExitQueued {
		t.Fatalf("notify: %+v", r)
	}
	s.advance(2 * time.Minute)
	return s, fk
}

func TestFlushThatLeavesASendQueuedExits75WithNext(t *testing.T) {
	s, _ := queuedPage(t)
	r := s.do("", "flush")
	expect(t, r.code == ExitQueued && firstLine(r.out) == "RESULT queued", "flush: %+v; want RESULT queued, exit 75", r)
	mustContain(t, "stdout", r.out, "phone: queued", "next: yo-human flush in 120s, 4 attempts left, then tell the human in your session")
	var obj struct {
		Status string
		Next   *struct {
			Do           string
			AttemptsLeft int `json:"attempts_left"`
			AfterSeconds int `json:"after_seconds"`
		}
	}
	s.advance(2 * time.Minute)
	r = s.do("", "flush", "--json")
	expect(t, r.code == ExitQueued, "flush --json: %+v; want exit 75", r)
	expect(t, json.Unmarshal([]byte(r.out), &obj) == nil && obj.Status == "queued" && obj.Next != nil && obj.Next.Do == "yo-human flush", "json = %s; want queued with next", r.out)
}

func TestFlushThatBuriesASendExits69(t *testing.T) {
	s, fk := queuedPage(t)
	failing(fk["pager"], channel.Permanent)
	r := s.do("", "flush")
	expect(t, r.code == ExitFailed && firstLine(r.out) == "RESULT failed", "flush: %+v; want RESULT failed, exit 69 (dead wins over the phone's queued send)", r)
	mustContain(t, "stdout", r.out, "pager: dead")
	expect(t, !strings.Contains(r.out, "next:"), "stdout = %q; a dead send has no next", r.out)
}

func TestFlushThatDeliversExitsZero(t *testing.T) {
	s, fk := queuedPage(t)
	fk["pager"].SendFunc, fk["phone"].SendFunc = nil, nil
	r := s.do("", "flush")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok", "flush: %+v; want RESULT ok", r)
	r = s.do("", "flush")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok", "empty flush: %+v", r)
}

func TestFlushWithOnlyParkedEntriesExitsZero(t *testing.T) {
	s, fk := notifySandbox(t)
	projectPage(t, s, fk) // queued under a config that is not the one in force
	for _, args := range [][]string{{"flush"}, {"flush", "--json"}} {
		r := s.do("", args...)
		expect(t, r.code == ExitOK && !strings.Contains(r.out, "next:") && !strings.Contains(r.out, `"next"`), "%v: %+v; a parked entry is not this run's to retry", args, r)
	}
	r := s.do("", "flush")
	expect(t, firstLine(r.out) == "RESULT ok", "flush: %q", r.out)
}

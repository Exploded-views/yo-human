package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

func TestNotifyStatusesAndExitCodes(t *testing.T) {
	type setup func(fk fakeSet)
	cases := []struct {
		name, level string
		setup       setup
		status      string
		code        int
	}{
		{"delivered", "page", func(fakeSet) {}, "delivered", ExitOK},
		{"partial", "page", func(fk fakeSet) { failing(fk["pager"], channel.Permanent) }, "partial", ExitOK},
		{"queued", "page", func(fk fakeSet) { failing(fk["pager"], channel.Transient); failing(fk["phone"], channel.Transient) }, "queued", ExitQueued},
		{"failed", "page", func(fk fakeSet) { failing(fk["pager"], channel.Permanent); failing(fk["phone"], channel.Permanent) }, "failed", ExitFailed},
		{"held", "night", func(fakeSet) {}, "held", ExitOK},
	}
	for _, c := range cases {
		s, fk := notifySandbox(t)
		c.setup(fk)
		r := s.notify(c.level)
		expect(t, r.code == c.code && r.err == "" && strings.HasPrefix(r.out, "RESULT "+c.status+" "), "%s: code=%d out=%q err=%q; want exit %d and RESULT %s first", c.name, r.code, r.out, r.err, c.code, c.status)
	}
	s, fk := notifySandbox(t)
	s.notify("page", "--dedup-key", "db-down")
	n := len(fk["pager"].Sent)
	r := s.notify("page", "--dedup-key", "db-down")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT suppressed "), "repeat: %+v; want suppressed, exit 0", r)
	expect(t, len(fk["pager"].Sent) == n, "a suppressed repeat sent again")
}

func TestHeldSendsWaitInTheQueueAndSendNothing(t *testing.T) {
	s, fk := notifySandbox(t)
	r := s.notify("night")
	mustContain(t, "stdout", r.out, "pager: held")
	expect(t, len(fk["pager"].Sent) == 0 && s.entries("queue") == 1, "a held send went out or was not queued: sent %d, queued %d", len(fk["pager"].Sent), s.entries("queue"))
}

func TestQueuedResultSaysWhatToDoNext(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	failing(fk["phone"], channel.Transient)
	r := s.notify("page")
	mustContain(t, "stdout", r.out, "next: yo-human flush in 60s, 5 attempts left, then tell the human in your session", "pager: queued")
	r = s.notify("page", "--json")
	var obj struct {
		Version  int
		Status   string
		ID       string
		Channels []struct{ Channel, Status string }
		Next     struct {
			Do           string
			AfterSeconds int `json:"after_seconds"`
			AttemptsLeft int `json:"attempts_left"`
			Then         string
		}
	}
	expect(t, r.code == ExitQueued && r.err == "" && json.Unmarshal([]byte(r.out), &obj) == nil, "json run: %+v", r)
	expect(t, obj.Version == 1 && obj.Status == "queued" && len(obj.ID) == 26 && len(obj.Channels) == 2, "object = %+v", obj)
	expect(t, obj.Next.Do == "yo-human flush" && obj.Next.AfterSeconds == 60 && obj.Next.AttemptsLeft == 5 && obj.Next.Then != "", "next = %+v", obj.Next)
}

func TestResultLineIsFirst(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	runs := map[string]result{
		"queued":     s.notify("page", "--dedup-key", "k1"),
		"held":       s.notify("night"),
		"delivered":  s.notify("note"),
		"resolve":    s.do("", "resolve", "--dedup-key", "k1"),
		"noop":       s.do("", "resolve", "--dedup-key", "never-seen"),
		"flush":      s.do("", "flush"),
		"status":     s.do("", "status"),
		"channeltst": s.do("", "channel", "test", "phone"),
	}
	for name, r := range runs {
		expect(t, resultLine.MatchString(firstLine(r.out)), "%s: first line %q does not match the RESULT grammar (code %d, err %q)", name, firstLine(r.out), r.code, r.err)
	}
	expect(t, runs["flush"].out != "" && firstLine(runs["flush"].out) == "RESULT ok", "flush prints RESULT ok, got %q", runs["flush"].out)
}

func TestNotifyBuildsOnlyTheChannelsItsLevelSendsTo(t *testing.T) {
	s, fk := notifySandbox(t)
	if err := os.Remove(s.secretsFile()); err != nil {
		t.Fatal(err)
	}
	s.setSecret("ntfy-topic") // pd-critical stays unset: the pager is unused by `note`
	r := s.notify("note")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT delivered "), "note: %+v; want delivered without the pager's secret", r)
	expect(t, len(fk["phone"].Sent) == 1 && len(fk["pager"].Sent) == 0, "phone sent %d, pager sent %d", len(fk["phone"].Sent), len(fk["pager"].Sent))
}

func TestNotifySendsTheNewMessageBeforeTheBacklog(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["phone"], channel.Transient)
	s.notify("note", "--title", "old") // queued for retry on the phone
	var order []string
	fk["phone"].SendFunc = nil
	s.advance(2 * time.Minute)
	n := len(fk["phone"].Sent)
	s.notify("note", "--title", "new")
	if len(fk["phone"].Sent) < n {
		t.FailNow()
	}
	for _, m := range fk["phone"].Sent[n:] {
		order = append(order, m.Title)
	}
	expect(t, strings.Join(order, ",") == "new,old", "send order = %v; want the new message first, then the backlog", order)
}

// A channel that rejects its configuration comes back with a result, and exit 78 follows it.
func TestNotifyWithAResultAndAConfigProblem(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Misconfigured)
	failing(fk["phone"], channel.Misconfigured)
	r := s.notify("page")
	expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT failed "), "text: %+v; want the result, then exit 78", r)
	mustContain(t, "stdout", r.out, "pager: dead")
	mustContain(t, "stderr", r.err, "error: channel", "rejected its configuration", "fix: ")
	r = s.notify("page", "--json")
	var obj struct {
		Status string
		Errors []struct{ What, Valid, Fix string }
	}
	expect(t, r.code == ExitConfig && r.err == "" && json.Unmarshal([]byte(r.out), &obj) == nil, "json: %+v", r)
	expect(t, obj.Status == "failed" && len(obj.Errors) == 1 && strings.Contains(obj.Errors[0].What, "rejected its configuration"), "object = %+v; want the result with the problem in errors", obj)
}

// A store failure after the send comes back with the result too, and exit 74 follows it.
func TestNotifyWithAResultAndAStoreProblem(t *testing.T) {
	s, fk := notifySandbox(t)
	fk["phone"].SendFunc = func(_ context.Context, _ channel.Message, spec channel.SendSpec) (channel.Receipt, error) {
		// The log the pipeline appends to becomes a directory, so the append fails and the send
		// itself succeeds. Sends run concurrently and the journal channel may recreate log.jsonl
		// between a remove and a mkdir, so retry until the directory is in place.
		lp := filepath.Join(s.stateDir(), "log.jsonl")
		var err error
		for range 100 {
			_ = os.RemoveAll(lp)
			if err = os.Mkdir(lp, 0o700); err == nil {
				return channel.Receipt{Instance: spec.Instance}, nil
			}
		}
		t.Errorf("could not make log.jsonl a directory after 100 tries: %v", err)
		return channel.Receipt{Instance: spec.Instance}, nil
	}
	r := s.notify("note")
	expect(t, r.code == ExitStore && strings.HasPrefix(r.out, "RESULT "), "text: %+v; want the result, then exit 74", r)
	mustContain(t, "stderr", r.err, "error: ")
	_ = os.RemoveAll(filepath.Join(s.stateDir(), "log.jsonl"))
	r = s.notify("note", "--json")
	var obj struct {
		Status string
		Errors []struct{ What string }
	}
	expect(t, r.code == ExitStore && r.err == "" && json.Unmarshal([]byte(r.out), &obj) == nil && obj.Status != "" && len(obj.Errors) > 0, "json: %+v object %+v", r, obj)
}

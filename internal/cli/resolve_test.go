package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

func TestResolveClosesTheIncidentAndNotesWhereItReached(t *testing.T) {
	s, fk := notifySandbox(t)
	s.notify("page", "--dedup-key", "db-down")
	r := s.do("", "resolve", "--dedup-key", "db-down", "--note", "fixed at 3am")
	expect(t, r.code == ExitOK && r.err == "" && strings.HasPrefix(r.out, "RESULT resolved "), "resolve: %+v", r)
	mustContain(t, "stdout", r.out, "phone: sent", "pager: sent")
	phone := fk["phone"].Sent
	expect(t, len(phone) == 2 && phone[1].Resolved && phone[1].Body == "fixed at 3am" && fk["phone"].Specs[1].Interruption == channel.Passive,
		"phone got %+v; want a passive resolve note carrying the note", phone)
	expect(t, len(fk["pager"].Resolved) == 1 && fk["pager"].Resolved[0] == "7:unknown/db-down", "pager resolved %v; want the project's key closed through the channel", fk["pager"].Resolved)
	mustContain(t, "status", s.do("", "status").out, "incidents: 0")
	removeSecrets(t, s, "ntfy-topic") // a closed incident builds no channel, so the pager's missing secret does not matter
	r = s.do("", "resolve", "--dedup-key", "db-down")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT noop "), "second resolve: %+v; want noop, exit 0", r)
	r = s.do("", "resolve", "--dedup-key", "never-seen", "--json")
	mustContain(t, "stdout", r.out, `"status":"noop"`, `"version":1`)
}

func TestResolveBuildsOnlyTheChannelsTheIncidentReached(t *testing.T) {
	s, _ := notifySandbox(t)
	s.notify("note", "--dedup-key", "fyi")
	if err := os.Remove(s.secretsFile()); err != nil {
		t.Fatal(err)
	}
	s.setSecret("ntfy-topic") // the pager's secret is gone, and the pager was never reached
	r := s.do("", "resolve", "--dedup-key", "fyi")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "resolve: %+v; want resolved without the pager's secret", r)
}

func TestResolveUsage(t *testing.T) {
	s, _ := notifySandbox(t)
	s.do("", "resolve").fails(t, "no key", ExitUsage, "--dedup-key")
	s.do("", "resolve", "--dedup-key", strings.Repeat("k", 256)).fails(t, "long key", ExitUsage, "255")
	s.do("", "resolve", "--dedup-key", "k", "stray").fails(t, "stray", ExitUsage, "takes no arguments")
	s.do("", "resolve", "--dedup-key", "k", "--bogus").fails(t, "unknown flag", ExitUsage, "--dedup-key")
	s.do("", "resolve", "--dedup-key", "k", "--note", strings.Repeat("a", 16385)).fails(t, "long note", ExitUsage, "16385 bytes > 16384")
	s.do("", "resolve", "--dedup-key", "k", "--note", "a\xffb").fails(t, "bad note", ExitUsage, "--note")
}

func TestResolveCancelsWhatIsStillQueued(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--dedup-key", "db-down")
	expect(t, s.entries("queue") == 1, "setup: the pager's send should be queued")
	fk["pager"].SendFunc = nil
	r := s.do("", "resolve", "--dedup-key", "db-down")
	mustContain(t, "stdout", r.out, "pager: cancelled")
	expect(t, s.entries("queue") == 0, "a resolved incident left a queued send")
}

// An incident silent for longer than incident_ttl is over: status no longer lists it, and resolve
// treats it the same way, as a noop that touches nothing.
func TestResolveOfAnExpiredIncidentIsANoop(t *testing.T) {
	s, fk := notifySandbox(t)
	s.notify("page", "--dedup-key", "old")
	s.advance(25 * time.Hour)
	mustContain(t, "status", s.do("", "status").out, "incidents: 0")
	r := s.do("", "resolve", "--dedup-key", "old")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT noop ") && len(fk["pager"].Resolved) == 0 && len(fk["phone"].Sent) == 1,
		"resolve: %+v; want noop, nothing closed or noted", r)
}

// An expired incident needs no channel: one whose secret is gone since is no problem.
func TestResolveOfAnExpiredIncidentBuildsNothing(t *testing.T) {
	s, _ := notifySandbox(t)
	s.notify("page", "--dedup-key", "old")
	s.advance(25 * time.Hour)
	removeSecrets(t, s, "ntfy-topic") // the pager's secret is gone
	r := s.do("", "resolve", "--dedup-key", "old")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT noop ") && r.err == "", "resolve: %+v; want a quiet noop", r)
}

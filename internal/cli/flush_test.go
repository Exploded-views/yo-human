package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

func TestFlushSendsWhatIsDue(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	failing(fk["phone"], channel.Transient)
	s.notify("page")
	fk["pager"].SendFunc, fk["phone"].SendFunc = nil, nil
	r := s.do("", "flush")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok" && !strings.Contains(r.out, "sent"), "too early: %+v; want RESULT ok and nothing sent (the retry is not due)", r)
	s.advance(2 * time.Minute)
	r = s.do("", "flush")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok", "flush: %+v", r)
	mustContain(t, "stdout", r.out, "phone: sent", "pager: sent")
	expect(t, s.entries("queue") == 0, "flush left %d entries queued", s.entries("queue"))
	r = s.do("", "flush", "--json")
	mustContain(t, "stdout", r.out, `"status":"ok"`, `"channels":[]`)
	s.do("", "flush", "stray").fails(t, "stray", ExitUsage, "takes no arguments")
}

func TestFlushBuildsOnlyTheChannelsOfItsDueEntries(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["phone"], channel.Transient)
	s.notify("note") // only the phone's send is queued
	fk["phone"].SendFunc = nil
	s.advance(2 * time.Minute)
	removeSecrets(t, s, "ntfy-topic") // the pager's secret is gone, and nothing is queued for the pager
	r := s.do("", "flush")
	expect(t, r.code == ExitOK && strings.Contains(r.out, "phone: sent"), "flush: %+v; want the phone's send without the pager's secret", r)
}

// An entry for a channel that cannot be built is left queued, and the failure is exit 78 beside
// the result for the entries that could be sent.
func TestFlushLeavesAnUnbuildableChannelsEntriesQueued(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	failing(fk["phone"], channel.Transient)
	s.notify("page")
	fk["pager"].SendFunc, fk["phone"].SendFunc = nil, nil
	s.advance(2 * time.Minute)
	removeSecrets(t, s, "ntfy-topic")
	r := s.do("", "flush")
	expect(t, r.code == ExitConfig && firstLine(r.out) == "RESULT ok", "flush: %+v; want the result, then exit 78", r)
	mustContain(t, "stdout", r.out, "phone: sent", "pager: queued")
	mustContain(t, "stderr", r.err, "yo-human secret set pd-critical")
	expect(t, s.entries("queue") == 1 && s.entries("dead") == 0, "queued %d, dead %d; want the pager's entry kept, none buried", s.entries("queue"), s.entries("dead"))
	r = s.do("", "flush", "--json")
	var obj struct {
		Status string
		Errors []struct{ What string }
	}
	expect(t, r.code == ExitConfig && r.err == "" && json.Unmarshal([]byte(r.out), &obj) == nil && len(obj.Errors) == 1, "json: %+v", r)
	s.setSecret("pd-critical")
	s.advance(time.Minute)
	r = s.do("", "flush")
	expect(t, r.code == ExitOK && strings.Contains(r.out, "pager: sent"), "after the fix: %+v", r)
}

func TestNotifyAlsoSendsTheBacklogOfOtherChannels(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--title", "older")
	fk["pager"].SendFunc = nil
	s.advance(2 * time.Minute)
	n := len(fk["pager"].Sent)
	r := s.notify("note") // the note level does not use the pager
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT delivered "), "notify: %+v", r)
	expect(t, len(fk["pager"].Sent) == n+1 && fk["pager"].Sent[len(fk["pager"].Sent)-1].Title == "older", "the pager got %d new sends; want the older page flushed by this call", len(fk["pager"].Sent)-n)
}

// A backlog channel that cannot be built does not fail a notify that delivered; the entry stays
// queued and the call says why on stderr.
func TestNotifyWarnsOfABacklogChannelItCannotBuild(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--title", "older")
	s.advance(2 * time.Minute)
	removeSecrets(t, s, "ntfy-topic")
	r := s.notify("note")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT delivered "), "notify: %+v", r)
	mustContain(t, "stderr", r.err, "warning:", "pd-critical")
	expect(t, s.entries("queue") == 1 && s.entries("dead") == 0, "queued %d, dead %d; want the pager's entry kept", s.entries("queue"), s.entries("dead"))
	r = s.notify("note", "--json")
	mustContain(t, "stdout", r.out, `"warnings":[`, "pd-critical")
	expect(t, r.err == "", "stderr under --json = %q, want empty", r.err)
}

// removeSecrets rewrites the secrets file to hold only the named secrets.
func removeSecrets(t *testing.T, s *sandbox, keep ...string) {
	t.Helper()
	write(t, s.secretsFile(), "")
	for _, k := range keep {
		s.setSecret(k)
	}
}

// An entry stranded in flight by a process that died is recovered before the channels are built,
// so the same flush sends it instead of parking it as "not built".
func TestFlushSendsARecoveredEntryAtOnce(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page")
	fk["pager"].SendFunc = nil
	strandInFlight(t, s)
	s.advance(2 * time.Hour)
	r := s.do("", "flush")
	expect(t, r.code == ExitOK && strings.Contains(r.out, "pager: sent") && len(fk["pager"].Sent) == 2, "flush: %+v; want the recovered entry sent", r)
}

// strandInFlight moves every queued entry to inflight/ under a pid that is gone, an hour old.
func strandInFlight(t *testing.T, s *sandbox) {
	t.Helper()
	q := filepath.Join(s.stateDir(), "queue")
	ents, err := os.ReadDir(q)
	if err != nil || len(ents) == 0 {
		t.Fatalf("queue: %v, %d entries", err, len(ents))
	}
	old := time.Now().Add(-time.Hour)
	for _, e := range ents {
		dst := filepath.Join(s.stateDir(), "inflight", strings.TrimSuffix(e.Name(), ".json")+".999999.json")
		if err := os.Rename(filepath.Join(q, e.Name()), dst); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dst, old, old); err != nil {
			t.Fatal(err)
		}
	}
}

// A recovery that fails (an unreadable entry in flight that cannot be set aside) stops the flush
// before it builds anything: exit 74.
func TestFlushStopsWhenRecoveryFails(t *testing.T) {
	s, _ := notifySandbox(t)
	s.do("", "status") // creates the state directory
	stale := filepath.Join(s.stateDir(), "inflight", "X1.pager.999999.json")
	write(t, stale, "not json")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	dead := filepath.Join(s.stateDir(), "dead")
	if err := os.Chmod(dead, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dead, 0o700) })
	s.do("", "flush").fails(t, "flush with a failing recovery", ExitStore, "error: ")
}

// So does a queue that cannot be listed (an unreadable entry that cannot be set aside).
func TestFlushStopsWhenTheQueueCannotBeListed(t *testing.T) {
	s, _ := notifySandbox(t)
	s.do("", "status") // creates the state directory
	write(t, filepath.Join(s.stateDir(), "queue", "X1.pager.json"), "not json")
	dead := filepath.Join(s.stateDir(), "dead")
	if err := os.Chmod(dead, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dead, 0o700) })
	s.do("", "flush").fails(t, "flush with an unlistable queue", ExitStore, "error: ")
}

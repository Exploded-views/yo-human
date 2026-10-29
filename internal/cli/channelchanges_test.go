package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// A channel the incident reached that cannot be built never stops the resolve: the incident is
// closed and its queued sends are cancelled; the failure is a problem printed after the result.
func TestResolveStillCancelsWhenAReachedChannelCannotBeBuilt(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--dedup-key", "db-down") // the phone is reached; the pager's send is queued
	removeSecrets(t, s, "pd-critical")         // the phone's secret is gone
	r := s.do("", "resolve", "--dedup-key", "db-down")
	expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT resolved "), "resolve: %+v; want the result, then exit 78", r)
	mustContain(t, "stdout", r.out, "pager: cancelled")
	mustContain(t, "stderr", r.err, "error: ", "ntfy-topic", "yo-human secret set ntfy-topic", "resolve note")
	mustNotContain(t, "stderr", r.err, "add a channels entry")
	fk["pager"].SendFunc = nil
	s.advance(2 * time.Minute)
	n := len(fk["pager"].Sent)
	s.do("", "flush")
	expect(t, s.entries("queue") <= 1 && len(fk["pager"].Sent) == n, "the pager was paged for a resolved incident (%d new sends)", len(fk["pager"].Sent)-n)
	r = s.do("", "resolve", "--dedup-key", "db-down", "--json")
	mustContain(t, "stdout", r.out, `"status":"noop"`)
}

func TestResolveStillCancelsWhenAReachedChannelIsNoLongerConfigured(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--dedup-key", "db-down")
	cfg := strings.Replace(notifyConfigBody, "      - { channel: phone, interruption: time-sensitive }\n", "", 1)
	cfg = strings.Replace(cfg, "      - { channel: phone, interruption: passive }\n", "      - { channel: pager, interruption: passive }\n", 1)
	cfg = strings.Replace(cfg, "  phone: { type: ntfy, server: \"https://ntfy.example.com\", topic: { secret: ntfy-topic } }\n", "", 1)
	write(t, s.userConfig(), cfg)
	r := s.do("", "resolve", "--dedup-key", "db-down")
	expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT resolved "), "resolve: %+v; want the result, then exit 78", r)
	mustContain(t, "stderr", r.err, "channel phone is no longer configured", "queued sends were cancelled")
	mustNotContain(t, "stderr", r.err, "add a channels entry")
	r = s.do("", "resolve", "--dedup-key", "db-down", "--json")
	mustContain(t, "stdout", r.out, `"status":"noop"`)
	fk["pager"].SendFunc = nil
	s.advance(2 * time.Minute)
	n := len(fk["pager"].Sent)
	s.do("", "flush")
	expect(t, len(fk["pager"].Sent) == n, "the pager was paged for a resolved incident")
}

const projectConfig = `version: 1
channels:
  pager2: { type: pagerduty, routing_key: { secret: pd-critical } }
levels:
  - name: proj
    use_when: "project page"
    send:
      - { channel: pager2, interruption: critical }
`

// projectPage queues a page under an explicit config that the user config does not know.
func projectPage(t *testing.T, s *sandbox, fk fakeSet) string {
	t.Helper()
	failing(fk["pager2"], channel.Transient)
	path := write(t, s.ownConfig("proj.yaml"), projectConfig)
	r := s.do("", "notify", "--level", "proj", "--title", "proj down", "--ask", "a", "--json", "--config", path)
	mustContain(t, "notify", r.out, `"status":"queued"`, `"do":"yo-human flush --config `)
	fk["pager2"].SendFunc = nil
	s.advance(2 * time.Minute)
	return path
}

// The queue is per user and the config per invocation: a flush under another config must not bury
// the page, and a flush under the right one sends it.
func TestFlushUnderAnotherConfigParksEntriesInsteadOfBurying(t *testing.T) {
	s, fk := notifySandbox(t)
	path := projectPage(t, s, fk)
	r := s.do("", "flush")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok", "flush: %+v; want ok (the entry belongs to another config)", r)
	mustContain(t, "stdout", r.out, "pager2: queued", "not configured")
	expect(t, s.entries("queue") == 1 && s.entries("dead") == 0 && len(fk["pager2"].Sent) == 1, "queued %d, dead %d; want the page kept", s.entries("queue"), s.entries("dead"))
	r = s.do("", "status")
	mustContain(t, "status", r.out, "queued: 1", "dead: 0", "reason: ", "not configured")
	r = s.do("", "notify", "--level", "note", "--title", "later", "--ask", "a")
	expect(t, r.code == ExitOK && r.err == "", "notify under the user config: %+v; want no noise about the other config's entry", r)
	r = s.do("", "flush", "--config", path)
	mustContain(t, "flush", r.out, "pager2: sent")
	expect(t, s.entries("queue") == 0 && len(fk["pager2"].Sent) == 2, "under its own config the page was not sent")
}

func TestFlushNeverSendsAnEntryToAChannelOfAnotherType(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["phone"], channel.Transient)
	cfg := strings.Replace(projectConfig, "pager2: { type: pagerduty, routing_key: { secret: pd-critical } }",
		"phone: { type: pagerduty, routing_key: { secret: pd-critical } }", 1)
	cfg = strings.Replace(cfg, "channel: pager2", "channel: phone", 1)
	path := write(t, s.ownConfig("proj.yaml"), cfg)
	s.do("", "notify", "--level", "proj", "--title", "proj down", "--ask", "a", "--config", path)
	fk["phone"].SendFunc = nil
	s.advance(2 * time.Minute)
	n := len(fk["phone"].Sent)
	r := s.do("", "flush") // the user config's phone is ntfy: another destination
	mustContain(t, "flush", r.out, "phone: queued")
	expect(t, len(fk["phone"].Sent) == n && s.entries("queue") == 1 && s.entries("dead") == 0, "the page went to a channel of another type or was buried (sent %d, queued %d, dead %d)", len(fk["phone"].Sent)-n, s.entries("queue"), s.entries("dead"))
}

// A relative --config is made absolute for the flush a queued result asks for, which may run from
// another directory.
func TestNextFlushNamesTheConfigByAnAbsolutePath(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager2"], channel.Transient)
	t.Chdir(s.configDir()) // a relative --config of the user's own, in their config directory
	write(t, "proj.yaml", projectConfig)
	r := s.do("", "notify", "--level", "proj", "--title", "t", "--ask", "a", "--json", "--config", "proj.yaml")
	wd, _ := os.Getwd()
	abs, err := filepath.EvalSymlinks(wd)
	expect(t, err == nil, "evalsymlinks: %v", err)
	mustContain(t, "notify", r.out, `"do":"yo-human flush --config `)
	expect(t, strings.Contains(r.out, "proj.yaml") && (strings.Contains(r.out, wd+"/proj.yaml") || strings.Contains(r.out, abs+"/proj.yaml")), "next.do in %q is not absolute", r.out)
}

// pageUnder queues a page on the phone under a project config whose phone is the given ntfy server.
func pageUnder(t *testing.T, s *sandbox, fk fakeSet, server string) string {
	t.Helper()
	failing(fk["phone"], channel.Transient)
	cfg := strings.Replace(notifyConfigBody, `server: "https://ntfy.example.com"`, `server: "`+server+`"`, 1)
	path := write(t, s.ownConfig("proj.yaml"), cfg)
	s.do("", "notify", "--level", "note", "--title", "from proj", "--ask", "a", "--config", path)
	fk["phone"].SendFunc = nil
	s.advance(2 * time.Minute)
	return path
}

// The same name and type in another config is another destination when its settings differ.
func TestFlushNeverSendsAnEntryToAnotherServerOfTheSameName(t *testing.T) {
	s, fk := notifySandbox(t)
	pageUnder(t, s, fk, "https://other-ntfy.example")
	n := len(fk["phone"].Sent)
	r := s.do("", "flush") // the user config's phone is https://ntfy.example.com
	mustContain(t, "flush", r.out, "phone: queued", "now points somewhere else than when this message was queued")
	expect(t, len(fk["phone"].Sent) == n && s.entries("queue") == 1 && s.entries("dead") == 0, "the message went to another server or was buried (sent %d)", len(fk["phone"].Sent)-n)
	mustContain(t, "status", s.do("", "status").out, "reason: ", "points somewhere else")
}

func TestFlushSendsAnEntryWhenTheChannelIsTheSame(t *testing.T) {
	s, fk := notifySandbox(t)
	pageUnder(t, s, fk, "https://ntfy.example.com") // identical settings under another file
	r := s.do("", "flush")
	mustContain(t, "flush", r.out, "phone: sent")
}

// Rotating a secret's value does not change where the message goes.
func TestFlushSendsAnEntryAfterASecretValueChanged(t *testing.T) {
	s, fk := notifySandbox(t)
	pageUnder(t, s, fk, "https://ntfy.example.com")
	if r := s.do("FAKE-ROTATED-TOPIC", "secret", "set", "ntfy-topic"); r.code != 0 {
		t.Fatal(r)
	}
	mustContain(t, "flush", s.do("", "flush").out, "phone: sent")
}

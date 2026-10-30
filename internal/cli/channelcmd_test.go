package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel/channeltest"

	"github.com/Exploded-views/yo-human/internal/channel"
)

type errString string

func (e errString) Error() string { return string(e) }

func TestChannelTestSendsOneTestMessage(t *testing.T) {
	s, fk := notifySandbox(t)
	r := s.do("", "channel", "test", "phone")
	expect(t, r.code == ExitOK && r.err == "" && regexpMatch(`^RESULT delivered [0-9A-Z]{26}$`, firstLine(r.out)), "channel test: %+v", r)
	mustContain(t, "stdout", r.out, "phone: sent")
	expect(t, len(fk["phone"].Sent) == 1 && len(fk["pager"].Sent) == 0 && len(fk["journal"].Sent) == 0, "sends: phone %d, pager %d, journal %d; want only the phone", len(fk["phone"].Sent), len(fk["pager"].Sent), len(fk["journal"].Sent))
	checkTestMessage(t, fk["phone"])
	_, err := os.Stat(filepath.Join(s.stateDir(), "incidents.json"))
	expect(t, err != nil && s.entries("queue") == 0, "a test touched the incidents or left a queue entry")
}

func checkTestMessage(t *testing.T, f *channeltest.Fake) {
	t.Helper()
	if len(f.Sent) != 1 || len(f.Specs) != 1 {
		t.Fatalf("sent %d messages; want 1", len(f.Sent))
	}
	m, spec := f.Sent[0], f.Specs[0]
	expect(t, strings.Contains(strings.ToLower(m.Title), "test") && m.Ask != "" && m.DedupKey == "" && m.ID != "", "message = %+v; want a titled test message with no dedup key", m)
	expect(t, spec.Interruption == channel.Passive && spec.Instance == "phone", "spec = %+v; want passive, for the phone", spec)
}

func TestChannelTestReportsAFailure(t *testing.T) {
	for _, c := range []struct {
		name string
		kind func(error) error
		code int
		line string
	}{
		{"permanent", channel.Permanent, ExitFailed, "phone: dead (provider said no)"},
		{"transient", channel.Transient, ExitFailed, "phone: transient (provider said no"},
	} {
		s, fk := notifySandbox(t)
		failing(fk["phone"], c.kind)
		r := s.do("", "channel", "test", "phone")
		expect(t, r.code == c.code && strings.HasPrefix(r.out, "RESULT failed "), "%s: %+v", c.name, r)
		mustContain(t, c.name+" stdout", r.out, c.line)
		expect(t, s.entries("queue") == 0 && s.entries("dead") == 0, "%s: a test left an entry behind", c.name)
	}
	s, fk := notifySandbox(t)
	failing(fk["phone"], channel.Misconfigured)
	r := s.do("", "channel", "test", "phone")
	expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT failed "), "config: %+v; want the result, then exit 78", r)
	mustContain(t, "stderr", r.err, "error: ", "rejected its configuration")
	r = s.do("", "channel", "test", "phone", "--json")
	mustContain(t, "stdout", r.out, `"status":"failed"`, `"errors":[`)
}

func TestChannelTestUsageAndConfigErrors(t *testing.T) {
	s, fk := notifySandbox(t)
	s.do("", "channel").fails(t, "no subcommand", ExitUsage, "subcommands")
	s.do("", "channel", "bogus").fails(t, "unknown subcommand", ExitUsage, "subcommands")
	s.do("", "channel", "test").fails(t, "no name", ExitUsage, "takes one channel name")
	s.do("", "channel", "test", "phone", "extra").fails(t, "two names", ExitUsage, "takes one channel name")
	s.do("", "channel", "test", "ghost").fails(t, "unknown channel", ExitConfig, `no channel named "ghost"`)
	removeSecrets(t, s, "ntfy-topic")
	s.do("", "channel", "test", "pager").fails(t, "missing secret", ExitConfig, "yo-human secret set pd-critical")
	expect(t, len(fk["phone"].Sent) == 0 && len(fk["pager"].Sent) == 0, "a failed test sent something")
}

// Every command but secret, config and version first sends what is due.
func TestChannelTestFlushesWhatIsDueFirst(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--title", "older")
	fk["pager"].SendFunc = nil
	s.advance(2 * time.Minute)
	n := len(fk["pager"].Sent)
	r := s.do("", "channel", "test", "phone")
	expect(t, r.code == ExitOK && len(fk["pager"].Sent) == n+1 && fk["pager"].Sent[len(fk["pager"].Sent)-1].Title == "older", "test: %+v; pager got %d new sends; want the older page flushed", r, len(fk["pager"].Sent)-n)
}

func TestChannelTestWithABrokenBacklogOnlyWarns(t *testing.T) {
	s, fk := notifySandbox(t)
	fk["pager"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{}, channel.Transient(errString("down"))
	}
	s.notify("page", "--title", "older")
	s.advance(2 * time.Minute)
	removeSecrets(t, s, "ntfy-topic")
	r := s.do("", "channel", "test", "phone")
	expect(t, r.code == ExitOK, "test: %+v; want the test's own outcome", r)
	mustContain(t, "stderr", r.err, "warning:", "pd-critical")
}

func regexpMatch(pattern, s string) bool { return regexp.MustCompile(pattern).MatchString(s) }

// A channel under test with an entry of its own due first sends that entry, through the same
// channel it built for the test.
func TestChannelTestFlushesTheChannelsOwnBacklogFirst(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["phone"], channel.Transient)
	s.notify("note")
	fk["phone"].SendFunc = nil
	s.advance(2 * time.Minute)
	r := s.do("", "channel", "test", "phone")
	expect(t, r.code == ExitOK && len(fk["phone"].Sent) == 3 && s.entries("queue") == 0, "channel test: %+v, phone sent %d; want the queued note and the test sent", r, len(fk["phone"].Sent))
}

func TestChannelTestTransientFailureSaysTryAgain(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["phone"], channel.Transient)
	r := s.do("", "channel", "test", "phone")
	expect(t, r.code == ExitFailed && strings.HasPrefix(r.out, "RESULT failed "), "transient: %+v", r)
	mustContain(t, "stdout", r.out, "phone: transient", "try again")
	failing(fk["phone"], channel.Permanent)
	r = s.do("", "channel", "test", "phone")
	mustContain(t, "stdout", r.out, "phone: dead")
	mustNotContain(t, "stdout", r.out, "try again")
}

// The flush before a test and the test itself run under one deadline.
func TestChannelTestSharesOneDeadlineWithItsFlush(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--title", "older")
	var flushDL, testDL time.Time
	fk["pager"].SendFunc = func(ctx context.Context, _ channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		flushDL, _ = ctx.Deadline()
		return channel.Receipt{}, nil
	}
	fk["phone"].SendFunc = func(ctx context.Context, _ channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		testDL, _ = ctx.Deadline()
		return channel.Receipt{}, nil
	}
	s.advance(2 * time.Minute)
	s.do("", "channel", "test", "phone")
	expect(t, !flushDL.IsZero() && flushDL.Equal(testDL), "flush deadline %v, test deadline %v; want one shared deadline", flushDL, testDL)
}

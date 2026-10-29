package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/channel/channeltest"
)

// notifyConfigBody is userConfigBody plus a level that holds in quiet hours, inside a window that
// never ends, on a channel that cannot schedule: its sends wait in the queue.
const notifyConfigBody = `version: 1
channels:
  phone: { type: ntfy, server: "https://ntfy.example.com", topic: { secret: ntfy-topic } }
  pager: { type: pagerduty, routing_key: { secret: pd-critical } }
  journal: { type: log, path: /tmp/yo-human-test.log }
quiet_hours:
  timezone: UTC
  windows: [ { days: [mon, tue, wed, thu, fri, sat, sun], from: "00:00", to: "00:00" } ]
levels:
  - name: note
    use_when: "FYI only"
    ask: optional
    send:
      - { channel: journal, interruption: passive }
      - { channel: phone, interruption: passive }
  - name: night
    use_when: "can wait until morning"
    quiet_hours: hold
    send:
      - { channel: pager, interruption: active }
  - name: page
    use_when: "production is down"
    send:
      - { channel: phone, interruption: time-sensitive }
      - { channel: pager, interruption: critical }
`

// fakeSet is one fake channel per instance name.
type fakeSet map[string]*channeltest.Fake

// notifySandbox is a sandbox with the notify config, the secrets of both network channels and a
// fake behind every instance, however the real type would have been built.
func notifySandbox(t *testing.T) (*sandbox, fakeSet) {
	t.Helper()
	s := newSandbox(t)
	s.clock = &testClock{t: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	clockHook = func() time.Time { return s.clock.t }
	t.Cleanup(func() { clockHook = nil })
	write(t, s.userConfig(), notifyConfigBody)
	s.setSecret("ntfy-topic")
	s.setSecret("pd-critical")
	return s, s.useFakes("journal", "phone", "pager", "pager2")
}

// testClock is the clock the CLI reads in a notify sandbox, so a test can move past a retry time.
type testClock struct{ t time.Time }

func (s *sandbox) advance(d time.Duration) { s.clock.t = s.clock.t.Add(d) }

func (s *sandbox) setSecret(name string) {
	s.t.Helper()
	if r := s.do("FAKE-"+strings.ToUpper(name), "secret", "set", name); r.code != 0 {
		s.t.Fatalf("secret set %s: %+v", name, r)
	}
}

// useFakes makes the registry build the given instances as fakes (a human-reaching one each, except
// the journal), keeping the real types' schemas and capabilities, and removes the hook afterwards.
func (s *sandbox) useFakes(names ...string) fakeSet {
	s.t.Helper()
	set := fakeSet{}
	for _, n := range names {
		set[n] = channeltest.NewFake(channel.Capabilities{ReachesHuman: n != "journal"})
	}
	registryHook = func(reg channel.Registry) {
		for name, typ := range reg {
			typ.New = func(in channel.Instance) (channel.Channel, error) {
				if f, ok := set[in.Name]; ok {
					return f, nil
				}
				return nil, errors.New("no fake for " + in.Name)
			}
			reg[name] = typ
		}
	}
	s.t.Cleanup(func() { registryHook = nil })
	return set
}

func (s *sandbox) stateDir() string { return filepath.Join(s.home, ".local", "state", "yo-human") }

// entries counts the files in a state subdirectory (queue, inflight or dead).
func (s *sandbox) entries(sub string) int {
	s.t.Helper()
	ents, err := os.ReadDir(filepath.Join(s.stateDir(), sub))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.t.Fatal(err)
	}
	return len(ents)
}

var resultLine = regexp.MustCompile(`^RESULT (delivered|partial|queued|failed|held|suppressed|resolved|noop|ok)( [0-9A-Z]{26})?$`)

// notify runs `notify --level <level> --title t --ask a` and any extra arguments.
func (s *sandbox) notify(level string, extra ...string) result {
	s.t.Helper()
	return s.do("", append([]string{"notify", "--level", level, "--title", "t", "--ask", "a"}, extra...)...)
}

// failing makes every send through f fail with the error made by kind.
func failing(f *channeltest.Fake, kind func(error) error) {
	f.SendFunc = func(_ context.Context, _ channel.Message, _ channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{}, kind(errors.New("provider said no"))
	}
}

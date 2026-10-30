package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

const schedConfig = `version: 1
channels:
  phone: { type: ntfy, server: "https://ntfy.example.com", topic: { secret: ntfy-topic } }
  pager: { type: pagerduty, routing_key: { secret: pd-critical } }
quiet_hours:
  timezone: UTC
  windows: [ { days: [mon, tue, wed, thu, fri, sat, sun], from: "11:00", to: "13:00" } ]
levels:
  - name: night
    use_when: "can wait"
    quiet_hours: hold
    send:
      - { channel: phone, interruption: active }
      - { channel: pager, interruption: active }
`

// scheduledPage leaves the phone holding a scheduled send for the "disk" incident (the fake clock is
// inside the quiet window, and ntfy can schedule), with the pager's copy held in the queue.
func scheduledPage(t *testing.T) (*sandbox, fakeSet) {
	t.Helper()
	s, fk := notifySandbox(t)
	write(t, s.userConfig(), schedConfig)
	fk["phone"].SendFunc = func(_ context.Context, _ channel.Message, sp channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{Instance: "phone", ProviderID: "sched-1", Scheduled: !sp.DeliverAt.IsZero()}, nil
	}
	r := s.do("", "notify", "--level", "night", "--title", "disk 80%", "--dedup-key", "disk", "--ask", "a")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT held "), "notify: %+v", r)
	return s, fk
}

func noPage(t *testing.T, fk fakeSet) {
	t.Helper()
	expect(t, len(fk["pager"].Sent) == 0 && len(fk["phone"].Sent) == 1, "pager sent %d, phone sent %d; want no page at any point, only the scheduled send", len(fk["pager"].Sent), len(fk["phone"].Sent))
}

func TestResolveQueuesTheCancelWhenTheChannelHasNoSecret(t *testing.T) {
	s, fk := scheduledPage(t)
	removeSecrets(t, s, "pd-critical") // the phone's secret is gone
	r := s.do("", "resolve", "--dedup-key", "disk")
	expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT resolved "), "resolve: %+v", r)
	mustContain(t, "stderr", r.err, "error: a scheduled send on phone could not be cancelled yet", "queued for cancellation", "yo-human flush")
	mustNotContain(t, "stderr", r.err, "report this as a bug", "unexpected error", "is not configured")
	mustContain(t, "status", s.do("", "status").out, "reason: ", "not built")
	s.advance(time.Hour)
	s.do("", "flush")
	expect(t, len(fk["phone"].Cancelled) == 0, "cancelled while the channel could not be built")
	s.setSecret("ntfy-topic")
	s.advance(time.Hour)
	r = s.do("", "flush")
	expect(t, r.code == ExitOK && len(fk["phone"].Cancelled) == 1 && fk["phone"].Cancelled[0].ProviderID == "sched-1", "flush: %+v; cancelled %v; want the scheduled send cancelled once", r, fk["phone"].Cancelled)
	s.do("", "flush")
	expect(t, len(fk["phone"].Cancelled) == 1, "cancelled again")
	noPage(t, fk)
}

func withoutPhone(body string) string {
	body = strings.Replace(body, "  phone: { type: ntfy, server: \"https://ntfy.example.com\", topic: { secret: ntfy-topic } }\n", "", 1)
	body = strings.Replace(body, "      - { channel: phone, interruption: active }\n", "", 1)
	return body
}

func TestResolveQueuesTheCancelWhenTheChannelWasRemovedAndRestored(t *testing.T) {
	s, fk := scheduledPage(t)
	write(t, s.userConfig(), withoutPhone(schedConfig))
	r := s.do("", "resolve", "--dedup-key", "disk")
	expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT resolved "), "resolve: %+v", r)
	mustContain(t, "stderr", r.err, "error: a scheduled send on phone could not be cancelled yet", "channel phone is no longer configured", "no resolve note is queued for it")
	mustNotContain(t, "stderr", r.err, "report this as a bug", "nothing is waiting")
	s.advance(time.Hour)
	s.do("", "flush")
	expect(t, len(fk["phone"].Cancelled) == 0, "cancelled through a config with no phone")
	write(t, s.userConfig(), schedConfig)
	s.advance(time.Hour)
	s.do("", "flush")
	expect(t, len(fk["phone"].Cancelled) == 1, "cancelled %d times after the channel was restored; want once", len(fk["phone"].Cancelled))
	noPage(t, fk)
}

func TestACancelIsNotRunThroughAChannelThatPointsElsewhere(t *testing.T) {
	s, fk := scheduledPage(t)
	write(t, s.userConfig(), withoutPhone(schedConfig))
	s.do("", "resolve", "--dedup-key", "disk")
	other := strings.Replace(schedConfig, "https://ntfy.example.com", "https://other-ntfy.example", 1)
	write(t, s.userConfig(), other)
	s.advance(time.Hour)
	r := s.do("", "flush")
	mustContain(t, "flush", r.out, "now points somewhere else")
	expect(t, len(fk["phone"].Cancelled) == 0, "the cancel went to a different server")
}

func TestARemovedChannelsResolveNoteIsSentOnlyWhenRestoredIdentically(t *testing.T) {
	s, fk := notifySandbox(t)
	s.notify("page", "--dedup-key", "db-down")
	cfg := strings.Replace(notifyConfigBody, "      - { channel: phone, interruption: time-sensitive }\n", "", 1)
	cfg = strings.Replace(cfg, "      - { channel: phone, interruption: passive }\n", "      - { channel: pager, interruption: passive }\n", 1)
	cfg = strings.Replace(cfg, "  phone: { type: ntfy, server: \"https://ntfy.example.com\", topic: { secret: ntfy-topic } }\n", "", 1)
	write(t, s.userConfig(), cfg)
	r := s.do("", "resolve", "--dedup-key", "db-down")
	mustContain(t, "stderr", r.err, "channel phone is no longer configured", "resolve note for it waits in the queue")
	mustNotContain(t, "stderr", r.err, "nothing is waiting")
	n := len(fk["phone"].Sent)
	write(t, s.userConfig(), strings.Replace(notifyConfigBody, "https://ntfy.example.com", "https://other-ntfy.example", 1))
	s.advance(time.Hour)
	mustContain(t, "flush", s.do("", "flush").out, "now points somewhere else")
	expect(t, len(fk["phone"].Sent) == n, "the note went to a different server")
	write(t, s.userConfig(), notifyConfigBody)
	s.advance(time.Hour)
	s.do("", "flush")
	sent := fk["phone"].Sent
	expect(t, len(sent) == n+1 && sent[n].Resolved, "phone got %d new messages; want the resolve note once the channel is restored identically", len(sent)-n)
}

// The note is only promised for a channel that delivered; a channel that only holds a scheduled
// send has no note waiting.
func TestNoNoteIsPromisedToAChannelThatOnlyHoldsAScheduledSend(t *testing.T) {
	s, _ := scheduledPage(t)
	removeSecrets(t, s, "pd-critical")
	r := s.do("", "resolve", "--dedup-key", "disk")
	mustContain(t, "stderr", r.err, "ntfy-topic")
	mustNotContain(t, "stderr", r.err, "resolve note")
}

type kindedEntry struct{ ID, Channel, Kind string }

type kindedStatus struct{ Queued, Held, Dead []kindedEntry }

func TestStatusLabelsEachEntryByKind(t *testing.T) {
	s, _ := scheduledPage(t)
	s.do("", "notify", "--level", "night", "--title", "other", "--dedup-key", "other", "--ask", "a") // a held page, for the pager
	removeSecrets(t, s, "pd-critical")
	s.do("", "resolve", "--dedup-key", "disk") // queues the scheduled send's cancellation
	r := s.do("", "status")
	mustContain(t, "status", r.out, "kind=cancel", "kind=send")
	var obj kindedStatus
	j := s.do("", "status", "--json")
	expect(t, j.code == ExitOK && json.Unmarshal([]byte(j.out), &obj) == nil, "json: %+v", j)
	kinds := map[string]bool{}
	for _, e := range append(append(obj.Queued, obj.Held...), obj.Dead...) {
		kinds[e.Kind] = true
	}
	expect(t, kinds["cancel"] && kinds["send"], "kinds in the JSON = %v; want cancel and send", kinds)
}

func TestStatusLabelsAResolveNote(t *testing.T) {
	s, _ := notifySandbox(t)
	s.notify("page", "--dedup-key", "db-down")
	cfg := strings.Replace(notifyConfigBody, "      - { channel: phone, interruption: time-sensitive }\n", "", 1)
	cfg = strings.Replace(cfg, "      - { channel: phone, interruption: passive }\n", "      - { channel: pager, interruption: passive }\n", 1)
	cfg = strings.Replace(cfg, "  phone: { type: ntfy, server: \"https://ntfy.example.com\", topic: { secret: ntfy-topic } }\n", "", 1)
	write(t, s.userConfig(), cfg)
	s.do("", "resolve", "--dedup-key", "db-down")
	mustContain(t, "status", s.do("", "status").out, "kind=note")
}

func TestStatusCallsAnEntryItCannotReadUnreadable(t *testing.T) {
	s, _ := notifySandbox(t)
	s.notify("note") // creates the state directory
	write(t, filepath.Join(s.stateDir(), "queue", "X1.phone.json"), `{"key":"X1.phone","incident_key":"","data":[1],"attempts":0,"next_attempt":"2026-10-30T12:00:00Z","deliver_at":"2026-10-30T12:00:00Z"}`)
	mustContain(t, "status", s.do("", "status").out, "dead: 1", "kind=unreadable")
}

// An expired incident's resolve is a noop, yet the page it still holds back is stopped: its
// scheduled send is cancelled and the held copy removed, so nothing fires when the window ends (R4).
func TestResolveOfAnExpiredIncidentCancelsItsScheduledSend(t *testing.T) {
	s, fk := scheduledPage(t)
	write(t, s.userConfig(), strings.Replace(schedConfig, "quiet_hours:", "incident_ttl: 30m\nquiet_hours:", 1))
	s.advance(40 * time.Minute) // past incident_ttl, still inside the hold window
	r := s.do("", "resolve", "--dedup-key", "disk")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT noop "), "resolve: %+v; want a noop", r)
	expect(t, len(fk["phone"].Cancelled) == 1 && fk["phone"].Cancelled[0].ProviderID == "sched-1", "cancelled %v; want the scheduled send cancelled", fk["phone"].Cancelled)
	s.advance(2 * time.Hour)
	s.do("", "flush")
	noPage(t, fk)
}

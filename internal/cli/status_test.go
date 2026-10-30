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

type statusObj struct {
	Version  int
	Status   string
	Inflight int
	Queued   []struct {
		ID, Channel string
		Attempts    int
		NextAttempt string `json:"next_attempt"`
	}
	Held []struct {
		ID, Channel string
		DeliverAt   string `json:"deliver_at"`
	}
	Dead      []struct{ ID, Channel, Reason, At string }
	Incidents []incidentObj
}

type incidentObj struct {
	Key, Level, Latest string
	Generation         int
	LastSent           string `json:"last_sent"`
}

// statusWorld leaves a queued send, a held send, a dead letter and an open incident.
func statusWorld(t *testing.T) (*sandbox, string, string, string) {
	t.Helper()
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	id := func(r result) string {
		f := strings.Fields(r.out)
		if len(f) < 3 {
			t.Fatalf("no result id in %+v", r)
		}
		return f[2]
	}
	queued := id(s.notify("page", "--dedup-key", "db-down", "--title", "db is down"))
	held := id(s.notify("night"))
	failing(fk["phone"], channel.Permanent)
	dead := id(s.notify("note"))
	return s, queued, held, dead
}

func TestStatusShowsQueuedHeldDeadIncidents(t *testing.T) {
	s, queued, held, dead := statusWorld(t)
	r := s.do("", "status")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok" && r.err == "", "status: %+v", r)
	mustContain(t, "stdout", r.out, "queued: 1", "held: 1", "dead: 1", "incidents: 1",
		"queued "+queued+" pager attempts=1", "held "+held+" pager until=", "dead "+dead+" phone", "provider said no",
		"incident db-down project=unknown level=page", "db is down")
	r = s.do("", "status", "--json")
	var obj statusObj
	expect(t, r.code == ExitOK && r.err == "" && json.Unmarshal([]byte(r.out), &obj) == nil, "json: %+v", r)
	checkStatusObject(t, obj, queued, held, dead)
}

// Another project's status shows none of it: the queue, the held sends, the dead letter and the
// incident all belong to project unknown.
func TestStatusOfAnotherProjectShowsNoneOfIt(t *testing.T) {
	s, _, _, _ := statusWorld(t)
	r := s.do("", "status", "--project", "other")
	expect(t, r.code == ExitOK, "status --project other: %+v", r)
	mustContain(t, "stdout", r.out, "queued: 0", "held: 0", "dead: 0", "incidents: 0")
}

func checkStatusObject(t *testing.T, obj statusObj, queued, held, dead string) {
	t.Helper()
	expect(t, obj.Version == 1 && obj.Status == "ok", "object = %+v", obj)
	if n := len(obj.Queued) + len(obj.Held)*10 + len(obj.Dead)*100 + len(obj.Incidents)*1000; n != 1111 {
		t.Fatalf("object = %+v; want one of each", obj)
	}
	q, h, d := obj.Queued[0], obj.Held[0], obj.Dead[0]
	expect(t, q.ID == queued && q.Channel == "pager" && q.Attempts == 1 && q.NextAttempt != "", "queued = %+v", q)
	expect(t, h.ID == held && h.DeliverAt != "", "held = %+v", h)
	expect(t, d.ID == dead && d.Channel == "phone" && d.At != "", "dead = %+v", d)
	expect(t, strings.Contains(d.Reason, "provider said no"), "dead reason = %q", d.Reason)
	checkIncident(t, obj.Incidents[0])
}

func checkIncident(t *testing.T, i incidentObj) {
	t.Helper()
	expect(t, i.Key == "db-down" && i.Level == "page" && i.Generation == 1, "incident = %+v", i)
	expect(t, i.Latest == "db is down" && i.LastSent != "", "incident = %+v", i)
}

func TestStatusIsEmptyAndNeedsNeitherSecretsNorChannels(t *testing.T) {
	s := newSandbox(t)
	s.withConfig() // no secrets stored, and no fakes: status builds no channel
	r := s.do("", "status", "--json")
	var obj statusObj
	expect(t, r.code == ExitOK && r.err == "" && json.Unmarshal([]byte(r.out), &obj) == nil, "status: %+v", r)
	mustContain(t, "stdout", r.out, `"queued":[]`, `"held":[]`, `"dead":[]`, `"incidents":[]`, `"inflight":0`)
	r = s.do("", "status")
	mustContain(t, "stdout", r.out, "queued: 0", "held: 0", "dead: 0", "incidents: 0")
	_, err := os.Stat(filepath.Join(s.stateDir(), "incidents.json"))
	expect(t, err != nil, "status wrote incidents.json")
	s.do("", "status", "stray").fails(t, "stray", ExitUsage, "takes no arguments")
}

func TestStatusDoesNotShowAnExpiredIncident(t *testing.T) {
	s, _, _, _ := statusWorld(t)
	s.advance(25 * time.Hour)
	mustContain(t, "stdout", s.do("", "status").out, "incidents: 0")
	s.advance(-25 * time.Hour)
	mustContain(t, "stdout", s.do("", "status").out, "incidents: 1")
}

func TestStatusShowsADueHeldEntryAsQueued(t *testing.T) {
	s, _, _, _ := statusWorld(t)
	s.advance(48 * time.Hour) // the held send's window ended
	mustContain(t, "stdout", s.do("", "status").out, "held: 0", "queued: 2")
}

func TestStatusSurvivesAHostileDedupKeyAndReason(t *testing.T) {
	s, fk := notifySandbox(t)
	fk["phone"].SendFunc = func(context.Context, channel.Message, channel.SendSpec) (channel.Receipt, error) {
		return channel.Receipt{}, channel.Permanent(errString("bad\nRESULT delivered 01ARZ3NDEKTSV4RRFFQ69G5FAV\x1b[2J"))
	}
	s.notify("note", "--dedup-key", "key with spaces")
	r := s.do("", "status")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok", "status: %+v", r)
	for _, l := range strings.Split(r.out, "\n")[1:] {
		expect(t, !strings.HasPrefix(l, "RESULT ") && !strings.Contains(l, "\x1b"), "status line %q forges a result or holds an escape", l)
	}
}

// Nothing in a hand-edited incident file can forge a line either.
func TestStatusFlattensWhatTheIncidentFileHolds(t *testing.T) {
	s, _ := notifySandbox(t)
	s.notify("note") // creates the state directory
	hostile := `{"k\u001b[2Jx\nRESULT delivered 01ARZ3NDEKTSV4RRFFQ69G5FAV": {"key": "k\u001b[2Jx\nRESULT delivered 01ARZ3NDEKTSV4RRFFQ69G5FAV", "generation": 1, "level": "page\u001b[2J", "open": true, ` +
		`"opened": "2026-10-30T12:00:00Z", "last_sent": "2026-10-30T12:00:00Z", "latest": "t\nRESULT delivered 01ARZ3NDEKTSV4RRFFQ69G5FAV\u001b[2J"}}`
	write(t, filepath.Join(s.stateDir(), "incidents.json"), hostile)
	r := s.do("", "status")
	expect(t, r.code == ExitOK && strings.Contains(r.out, "incidents: 1"), "status: %+v", r)
	for _, l := range strings.Split(r.out, "\n")[1:] {
		expect(t, !strings.HasPrefix(l, "RESULT ") && !strings.Contains(l, "\x1b"), "status line %q forges a result or holds an escape", l)
	}
}

func TestStatusFlushesWhatIsDueFirst(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--title", "older")
	fk["pager"].SendFunc = nil
	s.advance(2 * time.Minute)
	n := len(fk["pager"].Sent)
	r := s.do("", "status")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok" && len(fk["pager"].Sent) == n+1, "status: %+v; pager got %d new sends; want the older page flushed first", r, len(fk["pager"].Sent)-n)
	mustContain(t, "status", r.out, "queued: 0")
}

func TestStatusOnlyWarnsOfADueChannelItCannotBuild(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["pager"], channel.Transient)
	s.notify("page", "--title", "older")
	s.advance(2 * time.Minute)
	removeSecrets(t, s, "ntfy-topic")
	r := s.do("", "status")
	expect(t, r.code == ExitOK && firstLine(r.out) == "RESULT ok", "status: %+v", r)
	mustContain(t, "stderr", r.err, "warning:", "pd-critical")
	mustContain(t, "stdout", r.out, "queued: 1")
}

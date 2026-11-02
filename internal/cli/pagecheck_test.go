package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

func TestPageCheckPrintsTheMapping(t *testing.T) {
	s, fk := pageSandbox(t)
	r := s.do("", "page", "--check")
	expect(t, r.code == ExitOK && r.err == "", "page --check: %+v; want exit 0", r)
	mustContain(t, "stdout", r.out,
		"RESULT ok",
		`page:       level page, ask "look now"`,
		"sends: phone time-sensitive, pager critical",
		`page --low: level note, ask "look when you can"`,
		"sends: journal passive, phone passive",
		"agent: orchestrator")
	expect(t, strings.Index(r.out, "phone time-sensitive") < strings.Index(r.out, "pager critical"), "sends = %q; want config order", r.out)
	for name, f := range fk {
		expect(t, len(f.Sent) == 0 && len(f.Resolved) == 0, "channel %s was called by --check", name)
	}
}

func TestPageCheckJSON(t *testing.T) {
	s, _ := pageSandbox(t)
	r := s.do("", "page", "--check", "--json")
	var obj struct {
		Status  string
		Default struct {
			Level, Ask string
			Sends      []struct{ Channel, Interruption string }
		}
		Low struct {
			Level, Ask string
			Sends      []struct{ Channel, Interruption string }
		}
		Agent string
	}
	expect(t, r.code == ExitOK && json.Unmarshal([]byte(r.out), &obj) == nil, "page --check --json: %+v; want one object", r)
	mustContain(t, "stdout", r.out, `"default":{`, `"low":{`, `"agent":"orchestrator"`, `"sends":[{"channel":"phone","interruption":"time-sensitive"},{"channel":"pager","interruption":"critical"}]`)
	expect(t, obj.Status == "ok" && obj.Default.Level == "page" && obj.Default.Ask == "look now" && len(obj.Default.Sends) == 2 &&
		obj.Low.Level == "note" && obj.Low.Ask == "look when you can" && len(obj.Low.Sends) == 2 && obj.Low.Sends[0].Channel == "journal" && obj.Agent == "orchestrator",
		"object = %+v", obj)
}

func TestPageCheckAgentUnknownWhenUnset(t *testing.T) {
	s, _ := notifySandbox(t)
	write(t, s.userConfig(), notifyConfigBody+"shim:\n  default: { level: page, ask: \"a\" }\n  low: { level: note, ask: \"b\" }\n")
	r := s.do("", "page", "--check")
	expect(t, r.code == ExitOK && strings.Contains(r.out, "agent: unknown"), "page --check: %+v; want agent unknown", r)
}

// --check sends nothing, flushes nothing and leaves the queue as it was.
func TestPageCheckHasNoSideEffects(t *testing.T) {
	s, fk := pageSandbox(t)
	failing(fk["pager"], channel.Transient)
	failing(fk["phone"], channel.Transient)
	s.notify("page")
	fk["pager"].SendFunc, fk["phone"].SendFunc = nil, nil
	s.advance(2 * time.Minute) // the queued sends are due
	before := s.entries("queue")
	sent := len(fk["pager"].Sent) + len(fk["phone"].Sent)
	expect(t, before > 0, "fixture queued %d entries; want some due", before)
	r := s.do("", "page", "--check")
	expect(t, r.code == ExitOK, "page --check: %+v", r)
	expect(t, s.entries("queue") == before && len(fk["pager"].Sent)+len(fk["phone"].Sent) == sent,
		"queue %d -> %d, sends %d -> %d; want both unchanged", before, s.entries("queue"), sent, len(fk["pager"].Sent)+len(fk["phone"].Sent))
}

func TestPageCheckWithoutShimBlock(t *testing.T) {
	s, _ := notifySandbox(t)
	r := s.do("", "page", "--check")
	expect(t, r.code == ExitConfig && r.out == "", "page --check: %+v; want exit 78", r)
	mustContain(t, "stderr", r.err, "yo-human page needs a shim: block in the config, and there is none", "valid:", "fix:")
}

func TestPageCheckUsage(t *testing.T) {
	s, fk := pageSandbox(t)
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"positional": {[]string{"page", "--check", "x"}, "--check"},
		"resolve":    {[]string{"page", "--check", "--resolve", "k"}, "--resolve"},
		"low":        {[]string{"page", "--check", "--low"}, "--low"},
	} {
		r := s.do("", c.args...)
		expect(t, r.code == ExitUsage && r.out == "", "%s: %+v; want exit 64", name, r)
		mustContain(t, name+" stderr", r.err, "error:", "valid:", "fix:", c.want)
		mustNotContain(t, name+" stderr", r.err, "x\n")
	}
	expect(t, len(fk["pager"].Sent) == 0 && len(fk["phone"].Sent) == 0, "a rejected --check sent")
}

// Only instance names and interruptions: no server, topic, path or secret name or value.
func TestPageCheckShowsNoDestinationOrSecret(t *testing.T) {
	s, _ := pageSandbox(t)
	for _, args := range [][]string{{"page", "--check"}, {"page", "--check", "--json"}} {
		r := s.do("", args...)
		expect(t, r.code == ExitOK, "%v: %+v", args, r)
		mustNotContain(t, "stdout", r.out, "ntfy.example.com", "ntfy-topic", "pd-critical", "FAKE-NTFY-TOPIC", "FAKE-PD-CRITICAL", "yo-human-test.log", "https://")
	}
}

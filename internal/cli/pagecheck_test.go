package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
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

// An old caller's `page --version` must not page the human; a lone "version" or "help" is a request
// for information, not a summary, unless -- says otherwise.
func TestPageVersionAndHelpWordsSendNothing(t *testing.T) {
	s, fk := pageSandbox(t)
	for _, args := range [][]string{
		{"page", "--version"}, {"page", "-version"}, {"page", "-v"}, {"page", "version"}, {"page", "VERSION"},
		{"page", "help"}, {"page", "Help"}, {"page", "--low", "help"}, {"page", "--low", "-v"}, {"page", "--help"}, {"page", "-h"},
	} {
		r := s.do("", args...)
		want := ExitUsage
		if args[len(args)-1] == "--help" || args[len(args)-1] == "-h" {
			want = ExitOK
		}
		expect(t, r.code == want, "%v: %+v; want exit %d", args, r, want)
		expect(t, !strings.HasPrefix(r.out, "RESULT "+"delivered") && !strings.HasPrefix(r.out, "RESULT queued"), "%v: %+v; sent something", args, r)
	}
	r := s.do("", "page", "version")
	mustContain(t, "stderr", r.err, "error:", "--")
	for name, f := range fk {
		expect(t, len(f.Sent) == 0, "channel %s was called %d times", name, len(f.Sent))
	}
	expect(t, s.entries("queue") == 0, "queue holds %d entries", s.entries("queue"))
}

// Go's flag parsing stops at the first positional, so a flag typed after the summary would become
// the details or the key and page for real: `page "test" --check`, meant as a dry run, must not
// send. Leading whitespace must not get a flag word past the check either.
func TestPageFlagAfterTheSummarySendsNothing(t *testing.T) {
	s, fk := pageSandbox(t)
	for _, args := range [][]string{
		{"page", "x", "--check"}, {"page", "x", "--low"}, {"page", "x", "-low"}, {"page", "version", "--check"},
		{"page", "version", "--help"}, {"page", "version", "--version"}, {"page", "x", "details", "--low"},
		{"page", "x", "--resolve=k"}, {"page", "x", "-h"}, {"page", " --version"}, {"page", " -h"},
		{"page", "x", " --CHECK "}, {"page", "x", "-json"}, {"page", "x", "--JSON"}, {"page", "x", "--json=true"},
		{"page", "x", "-config", "f"},
	} {
		r := s.do("", args...)
		expect(t, r.code == ExitUsage, "%v: %+v; want exit 64", args, r)
	}
	r := s.do("", "page", "x", "--check")
	mustContain(t, "stderr", r.err, "error:", "among its arguments", "before the summary", "--")
	for name, f := range fk {
		expect(t, len(f.Sent) == 0, "channel %s was called %d times", name, len(f.Sent))
	}
	expect(t, s.entries("queue") == 0, "queue holds %d entries", s.entries("queue"))
}

// After --, a flag word is text on purpose.
func TestPageFlagWordAfterDashDashIsText(t *testing.T) {
	s, fk := pageSandbox(t)
	r := s.do("", "page", "--", "x", "--check")
	expect(t, r.code == ExitOK && len(fk["pager"].Sent) == 1 && fk["pager"].Sent[0].Body == "--check", "page -- x --check: %+v; want one send with details --check", r)
}

func TestPageAfterDashDashSendsTheWord(t *testing.T) {
	s, fk := pageSandbox(t)
	r := s.do("", "page", "--", "version")
	expect(t, r.code == ExitOK && len(fk["pager"].Sent) == 1 && fk["pager"].Sent[0].Title == "version", "page -- version: %+v, pager %d; want one send titled version", r, len(fk["pager"].Sent))
	r = s.do("", "page", "--low", "--", "help")
	expect(t, r.code == ExitOK && len(fk["phone"].Sent) == 2 && fk["phone"].Sent[1].Title == "help", "page --low -- help: %+v; want one send", r)
}

// A mode with no level is a config problem naming that mode; a level the config does not have
// (the loader refuses one, so this is built by hand) is one too.
func TestPageCheckModeProblems(t *testing.T) {
	levels := []config.Level{{Name: "page"}}
	for _, c := range []struct {
		name string
		shim config.Shim
		want string
	}{
		{"default without a level", config.Shim{Low: config.ShimMode{Level: "page"}}, "shim.default.level is not set"},
		{"low without a level", config.Shim{Default: config.ShimMode{Level: "page"}}, "shim.low.level is not set"},
		{"an unknown level", config.Shim{Default: config.ShimMode{Level: "gone"}, Low: config.ShimMode{Level: "page"}}, "shim.default names no level"},
	} {
		shim := c.shim
		var out, errb strings.Builder
		e := &env{stdout: &out, stderr: &errb}
		code := e.pageCheck(&config.Config{Levels: levels, Shim: &shim})
		if code != ExitConfig || !strings.Contains(errb.String(), c.want) || out.String() != "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit 78 naming %q", c.name, code, out.String(), errb.String(), c.want)
		}
	}
}

// A level with no sends (built by hand; the loader requires one) shows `sends: none`, not an empty list.
func TestPageCheckLevelWithNoSends(t *testing.T) {
	var out, errb strings.Builder
	e := &env{stdout: &out, stderr: &errb}
	shim := config.Shim{Default: config.ShimMode{Level: "quiet"}, Low: config.ShimMode{Level: "quiet"}}
	code := e.pageCheck(&config.Config{Levels: []config.Level{{Name: "quiet"}}, Shim: &shim})
	if code != ExitOK || strings.Count(out.String(), "sends: none") != 2 {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 0 and `sends: none` for both modes", code, out.String(), errb.String())
	}
}

package cli

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// shimBlock maps the old script's two modes onto the notify config's levels.
const shimBlock = `shim:
  default: { level: page, ask: "look now" }
  low:     { level: note, ask: "look when you can" }
  agent: orchestrator
`

// pageSandbox is a notify sandbox whose config also has the shim block.
func pageSandbox(t *testing.T) (*sandbox, fakeSet) {
	t.Helper()
	s, fk := notifySandbox(t)
	write(t, s.userConfig(), notifyConfigBody+shimBlock)
	t.Setenv("YO_HUMAN_AGENT", "")
	return s, fk
}

func TestPageTrigger(t *testing.T) {
	s, fk := pageSandbox(t)
	r := s.do("", "page", "db down", "details", "key1")
	expect(t, r.code == ExitOK && r.err == "" && strings.HasPrefix(r.out, "RESULT delivered "), "page: %+v; want RESULT delivered first, exit 0", r)
	sent := fk["pager"].Sent
	expect(t, len(sent) == 1, "pager got %d sends; want 1", len(sent))
	if len(sent) == 1 {
		m := sent[0]
		expect(t, m.Level == "page" && m.Ask == "look now" && m.Title == "db down" && m.Body == "details" && m.DedupKey == "7:unknown/key1" && m.Agent == "orchestrator",
			"message = %+v; want level page, ask, title, body, key and agent from the shim", m)
	}
	t.Setenv("YO_HUMAN_AGENT", "builder-7")
	s.do("", "page", "again", "d", "key2")
	sent = fk["pager"].Sent
	expect(t, len(sent) == 2 && sent[1].Agent == "builder-7", "sends = %+v; want YO_HUMAN_AGENT to win over shim.agent", sent)
}

func TestPageLow(t *testing.T) {
	s, fk := pageSandbox(t)
	r := s.do("", "page", "--low", "ci broken", "log", "ci-key")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT delivered "), "page --low: %+v", r)
	sent := fk["phone"].Sent
	expect(t, len(sent) == 1 && sent[0].Level == "note" && sent[0].Ask == "look when you can" && sent[0].Title == "ci broken" && len(fk["pager"].Sent) == 0,
		"phone got %+v, pager %d; want one note-level send", sent, len(fk["pager"].Sent))
}

func TestPageDefaultDedupKey(t *testing.T) {
	s, fk := pageSandbox(t)
	s.clock.t = time.Date(2026, 10, 30, 12, 34, 56, 0, time.FixedZone("EST", -5*3600))
	s.do("", "page", "db down")
	s.do("", "page", "db down 2", "details")
	got := fk["pager"].Sent
	want := regexp.MustCompile(`^7:unknown/orch-20261030T173456Z-[0-9a-f]{4}$`)
	expect(t, len(got) == 2 && want.MatchString(got[0].DedupKey) && want.MatchString(got[1].DedupKey) && got[0].Body == "", "keys = %+v; want %s (UTC, from the clock), empty body when none given", got, want)
}

func TestPageResolve(t *testing.T) {
	s, fk := pageSandbox(t)
	s.do("", "page", "db down", "d", "key1")
	r := s.do("", "page", "--resolve", "key1")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "page --resolve: %+v", r)
	expect(t, len(fk["pager"].Resolved) == 1 && fk["pager"].Resolved[0] == "7:unknown/key1", "pager resolved %v; want 7:unknown/key1, the project's key", fk["pager"].Resolved)
	phone := fk["phone"].Sent
	expect(t, len(phone) == 2 && phone[1].Resolved && phone[1].Body == "", "phone got %+v; want a resolve note with no text of its own", phone)
	s.do("", "page", "--low", "ci", "d", "key2")
	r = s.do("", "page", "--low", "--resolve", "key2")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "page --low --resolve: %+v", r)
	mustContain(t, "status", s.do("", "status").out, "incidents: 0")
}

func TestPageWithoutShimBlock(t *testing.T) {
	s, _ := notifySandbox(t)
	for _, args := range [][]string{{"page", "x"}, {"page", "--resolve", "k"}} {
		r := s.do("", args...)
		expect(t, r.code == ExitConfig && r.out == "", "%v: %+v; want exit 78", args, r)
		mustContain(t, "stderr", r.err, "shim:", "error:", "valid:", "fix:")
	}
}

func TestPageUsage(t *testing.T) {
	s, fk := pageSandbox(t)
	long := strings.Repeat("k", 256)
	cases := []struct {
		name string
		args []string
		echo string
	}{
		{"missing summary", []string{"page"}, ""},
		{"blank summary", []string{"page", "  "}, ""},
		{"summary of 201 characters", []string{"page", strings.Repeat("s", 201)}, ""},
		{"resolve without key", []string{"page", "--resolve"}, ""},
		{"resolve with an empty key", []string{"page", "--resolve", ""}, ""},
		{"resolve with a summary", []string{"page", "--resolve", "k", "extra"}, ""},
		{"too many arguments", []string{"page", "a", "b", "c", "d"}, ""},
		{"unknown flag", []string{"page", "--bogus", "a"}, "bogus"},
		{"key of 256 characters", []string{"page", "a", "b", long}, long},
		{"resolve key of 256 characters", []string{"page", "--resolve", long}, long},
		{"invalid utf-8 key", []string{"page", "a", "b", "k\xff"}, "k\xff"},
		{"invalid utf-8 summary", []string{"page", "a\xff"}, "a\xff"},
		{"body over 16 KB", []string{"page", "a", strings.Repeat("b", 16385)}, strings.Repeat("b", 100)},
		{"control character in key", []string{"page", "a", "b", "k\x07"}, "k\x07"},
	}
	for _, c := range cases {
		r := s.do("", c.args...)
		expect(t, r.code == ExitUsage && r.out == "", "%s: %+v; want exit 64 and no stdout", c.name, r)
		mustContain(t, c.name+" stderr", r.err, "error:", "valid:", "fix:")
		mustNotContain(t, c.name+" stderr", r.err, "--title", "--body", "--dedup-key", "--link", "--ask")
		if c.echo != "" {
			mustNotContain(t, c.name+" stderr", r.err, c.echo)
		}
	}
	expect(t, len(fk["pager"].Sent) == 0 && len(fk["phone"].Sent) == 0, "a rejected page was sent")
}

func TestPageHalfConfiguredShim(t *testing.T) {
	s, _ := notifySandbox(t)
	write(t, s.userConfig(), notifyConfigBody+"shim:\n  default: { level: page, ask: x }\n")
	r := s.do("", "page", "--low", "x")
	expect(t, r.code == ExitConfig && r.out == "", "low with no shim.low: %+v; want exit 78", r)
	mustContain(t, "stderr", r.err, "shim.low.level")
}

func TestPageJSONAndFlatSummary(t *testing.T) {
	s, fk := pageSandbox(t)
	r := s.do("", "page", "line one\nline two", "d", "k", "--json")
	mustContain(t, "stdout", r.out, `"status":"delivered"`, `"version":1`)
	sent := fk["pager"].Sent
	expect(t, len(sent) == 1 && sent[0].Title == "line one line two", "sends = %+v; want a one-line title", sent)
}

func TestPageAgentIsUnknownWhenNeitherNamesOne(t *testing.T) {
	s, fk := notifySandbox(t)
	write(t, s.userConfig(), notifyConfigBody+"shim:\n  default: { level: page, ask: x }\n  low: { level: note, ask: x }\n")
	t.Setenv("YO_HUMAN_AGENT", "")
	s.do("", "page", "x")
	sent := fk["pager"].Sent
	expect(t, len(sent) == 1 && sent[0].Agent == "unknown", "sends = %+v; want agent unknown", sent)
}

func TestPageWithAnUnreadableConfigFailsAsAConfigProblem(t *testing.T) {
	s, _ := pageSandbox(t)
	r := s.do("", "page", "x", "--config", "/nonexistent/FAKE-config.yaml")
	expect(t, r.code == ExitConfig && r.out == "", "page with a missing --config: %+v; want exit 78", r)
}

func TestPageNamesItsOwnArgumentsInUsageErrors(t *testing.T) {
	s, _ := pageSandbox(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"page", " "}, "the summary (first argument)"},
		{[]string{"page", strings.Repeat("s", 201)}, "the summary (first argument)"},
		{[]string{"page", "a", "b", strings.Repeat("k", 256)}, "the dedup key (third argument)"},
		{[]string{"page", "--resolve", strings.Repeat("k", 256)}, "the dedup key after --resolve"},
		{[]string{"page", "a", strings.Repeat("b", 16385)}, "the details (second argument)"},
		{[]string{"page"}, `use -- before a summary that starts with "-"`},
		{[]string{"page", "--bogus", "x"}, `use -- before a summary that starts with "-"`},
		{[]string{"page", strings.Repeat("s", 201)}, "put the detail in the details (second argument)"},
	}
	for _, c := range cases {
		r := s.do("", c.args...)
		expect(t, r.code == ExitUsage, "%v: code %d; want 64", c.args, r.code)
		mustContain(t, "stderr", r.err, c.want)
	}
}

func TestPageWithoutAnAskOnALevelThatNeedsOneIsAConfigProblem(t *testing.T) {
	for _, low := range []bool{false, true} {
		s, fk := notifySandbox(t)
		write(t, s.userConfig(), notifyConfigBody+"shim:\n  default: { level: page }\n  low: { level: page }\n")
		args := []string{"page", "x"}
		path := "shim.default.ask"
		if low {
			args, path = []string{"page", "--low", "x"}, "shim.low.ask"
		}
		r := s.do("", args...)
		expect(t, r.code == ExitConfig && r.out == "", "%v: %+v; want exit 78", args, r)
		mustContain(t, "stderr", r.err, "fix: set "+path+" in the shim: block")
		mustNotContain(t, "stderr", r.err, "--ask")
		expect(t, len(fk["pager"].Sent) == 0, "a page with no ask was sent")
	}
}

func TestPageEmptyKeyArgumentIsTheDefaultKey(t *testing.T) {
	s, fk := pageSandbox(t)
	s.do("", "page", "x", "d", "")
	sent := fk["pager"].Sent
	expect(t, len(sent) == 1 && regexp.MustCompile(`^7:unknown/orch-20261030T120000Z-[0-9a-f]{4}$`).MatchString(sent[0].DedupKey), "sends = %+v; want the default key for an empty third argument", sent)
}

func TestPageAskIsOneLine(t *testing.T) {
	s, fk := notifySandbox(t)
	write(t, s.userConfig(), notifyConfigBody+"shim:\n  default: { level: page, ask: \"look\\nnow\" }\n  low: { level: note, ask: x }\n")
	s.do("", "page", "x")
	sent := fk["pager"].Sent
	expect(t, len(sent) == 1 && sent[0].Ask == "look now", "sends = %+v; want the ask on one line", sent)
}

// The caller must be able to resolve what it paged: the key, chosen or default, is printed.
func TestPagePrintsItsKey(t *testing.T) {
	s, _ := pageSandbox(t)
	r := s.do("", "page", "db down")
	m := regexp.MustCompile(`(?m)^key: (orch-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4})$`).FindStringSubmatch(r.out)
	expect(t, r.code == ExitOK && m != nil, "page: %+v; want a key: line with the default key", r)
	if m != nil {
		expect(t, strings.HasPrefix(s.do("", "page", "--resolve", m[1]).out, "RESULT resolved"), "the printed key does not resolve the page")
	}
	r = s.do("", "page", "db down", "", "my-key", "--json")
	mustContain(t, "stdout", r.out, `"dedup_key":"my-key"`)
}

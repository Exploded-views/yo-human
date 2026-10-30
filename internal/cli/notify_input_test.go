package cli

import (
	"os"
	"strings"
	"testing"
)

func TestNotifyInputLimits(t *testing.T) {
	good := "https://example.com/ok"
	actions := []string{"--action", "a=" + good, "--action", "b=" + good, "--action", "c=" + good}
	cases := []struct {
		name, stdin string
		args        []string
		want        string
		notEchoed   string
	}{
		{"body over the limit", strings.Repeat("a", 16385), []string{"--body", "-"}, "16385 bytes > 16384", ""},
		{"body that is not UTF-8", "ab\xff\xfecd", []string{"--body", "-"}, "UTF-8", ""},
		{"a fourth action", "", append(append([]string{}, actions...), "--action", "d="+good), "at most 3", ""},
		{"an ftp link", "", []string{"--link", "ftp://files.example/secret-path"}, "--link", "files.example"},
		{"a link with credentials", "", []string{"--link", "https://user:hunter2" + "@example.com/"}, "--link", "hunter2"}, // split in the source so the public-repo scrub stays uniform
		{"a link without a host", "", []string{"--link", "https:///path"}, "--link", ""},
		{"an ftp action", "", []string{"--action", "get=ftp://files.example/private"}, "--action", "files.example"},
		{"an action without a label", "", []string{"--action", "=" + good}, "--action", ""},
		{"an action label that is not UTF-8", "", []string{"--action", "a\xffb=" + good}, "--action", ""},
		{"an action without an equals sign", "", []string{"--action", "nolabel"}, "--action", "nolabel"},
		{"a 256-character dedup key", "", []string{"--dedup-key", strings.Repeat("k", 256)}, "--dedup-key", strings.Repeat("k", 20)},
		{"a dedup key with a control character", "", []string{"--dedup-key", "a\x1b[2Jb"}, "--dedup-key", "[2J"},
		{"a dedup key that is not UTF-8", "", []string{"--dedup-key", "a\xffb"}, "--dedup-key", ""},
		{"a body file that is a directory", "", []string{"--body-file", os.TempDir()}, "--body-file", ""},
		{"a 201-character title", "", []string{"--title", strings.Repeat("é", 201)}, "200", ""},
		{"an ask that is not UTF-8", "", []string{"--ask", "a\xffb"}, "--ask", ""},
		{"a title that is not UTF-8", "", []string{"--title", "a\xffb"}, "--title", ""},
		{"a body from two places", "x", []string{"--body", "-", "--body-file", os.Args[0]}, "both given", ""},
		{"an unreadable body file", "", []string{"--body-file", "/nonexistent/body.txt"}, "--body-file", ""},
		{"an unknown flag", "", []string{"--bogus", "x"}, "--level", "bogus"},
		{"a flag that needs a value", "", []string{"--link"}, "--link", ""},
		{"a stray argument", "", []string{"stray"}, "takes no arguments", ""},
	}
	for _, c := range cases {
		s, fk := notifySandbox(t)
		r := s.do(c.stdin, append([]string{"notify", "--level", "note", "--title", "t"}, c.args...)...)
		r.fails(t, c.name, ExitUsage, c.want)
		expect(t, c.notEchoed == "" || !strings.Contains(r.err, c.notEchoed), "%s: stderr %q echoes %q", c.name, r.err, c.notEchoed)
		expect(t, s.entries("queue") == 0 && len(fk["phone"].Sent) == 0, "%s: something was enqueued or sent", c.name)
	}
}

func TestNotifyAcceptsTheLimits(t *testing.T) {
	s, fk := notifySandbox(t)
	good := "https://example.com/ok"
	r := s.do(strings.Repeat("a", 16384), "notify", "--level", "note", "--title", strings.Repeat("é", 200),
		"--dedup-key", strings.Repeat("k", 255), "--body", "-", "--link", good,
		"--action", "a="+good+"?x=1", "--action", "b="+good, "--action", "c="+good)
	expect(t, r.code == 0 && firstLine(r.out) != "", "at the limits: %+v", r)
	m := fk["phone"].Sent
	expect(t, len(m) == 1 && len(m[0].Body) == 16384 && len(m[0].Actions) == 3 && m[0].Actions[0].URL == good+"?x=1" && m[0].Actions[0].Label == "a",
		"message = %+v; want the body and three actions intact (an equals sign in the URL stays in the URL)", m)
}

func TestNotifyFlattensTitleAndAsk(t *testing.T) {
	s, fk := notifySandbox(t)
	r := s.do("", "notify", "--level", "note", "--title", "line one\nline two\x1b[2J", "--ask", "do\nthis")
	expect(t, r.code == 0, "run: %+v", r)
	m := fk["phone"].Sent
	expect(t, len(m) == 1 && m[0].Title == "line one line two [2J" && m[0].Ask == "do this", "message = %+v; want newlines and escapes flattened to spaces", m)
}

func TestNotifyReadsTheBodyFromAFileOrStdin(t *testing.T) {
	s, fk := notifySandbox(t)
	file := write(t, "body.txt", "from a file\nsecond line")
	expect(t, s.do("", "notify", "--level", "note", "--title", "t", "--body-file", file).code == 0, "body-file")
	expect(t, s.do("from stdin", "notify", "--level", "note", "--title", "t", "--body", "-").code == 0, "stdin")
	expect(t, s.do("", "notify", "--level", "note", "--title", "t", "--body", "literal").code == 0, "literal")
	var got []string
	for _, m := range fk["phone"].Sent {
		got = append(got, m.Body)
	}
	expect(t, strings.Join(got, "|") == "from a file\nsecond line|from stdin|literal", "bodies = %q", got)
	big := write(t, "big.txt", strings.Repeat("a", 16385))
	s.do("", "notify", "--level", "note", "--title", "t", "--body-file", big).fails(t, "big file", ExitUsage, "16385 bytes > 16384")
	huge := write(t, "huge.txt", strings.Repeat("a", 2<<20))
	r := s.do("", "notify", "--level", "note", "--title", "t", "--body-file", huge)
	r.fails(t, "huge file", ExitUsage, "over 1048576 bytes > 16384")
}

func TestNotifyRequiredFlags(t *testing.T) {
	s, _ := notifySandbox(t)
	s.do("", "notify", "--title", "t").fails(t, "no level", ExitUsage, "--level is missing")
	s.do("", "notify", "--level", "note").fails(t, "no title", ExitUsage, "--title")
	s.do("", "notify", "--level", "note", "--title", " \n ").fails(t, "blank title", ExitUsage, "--title")
	r := s.do("", "notify", "--level", "urgent", "--title", "t")
	r.fails(t, "unknown level", ExitUsage, `unknown level "urgent"`)
	mustContain(t, "stderr", r.err, "note (use when FYI only)", "page (use when production is down)")
}

func TestNotifyAskRequiredUnlessOptional(t *testing.T) {
	s, fk := notifySandbox(t)
	r := s.do("", "notify", "--level", "page", "--title", "t")
	r.fails(t, "page without ask", ExitUsage, "--ask")
	mustContain(t, "stderr", r.err, `fix: `, `--ask "<what the human must do>"`)
	s.do("", "notify", "--level", "page", "--title", "t", "--ask", "  ").fails(t, "blank ask", ExitUsage, "--ask")
	expect(t, s.entries("queue") == 0 && len(fk["phone"].Sent) == 0, "a usage error enqueued or sent something")
	expect(t, s.do("", "notify", "--level", "note", "--title", "t").code == 0, "note is ask: optional")
}

func TestDefaultsFromEnv(t *testing.T) {
	s, fk := notifySandbox(t)
	s.notify("note")
	s.notify("note", "--project", "flag-project", "--agent", "flag-agent")
	s.notify("note", "--project", "two\nlines", "--agent", " \n ")
	t.Setenv("YO_HUMAN_PROJECT", "billing")
	t.Setenv("YO_HUMAN_AGENT", "reviewer")
	s.notify("note")
	s.notify("note", "--project", "billing", "--agent", " \n ") // --project may name this project; a blank agent falls back
	var got []string
	for _, m := range fk["phone"].Sent {
		got = append(got, m.Project+"/"+m.Agent)
	}
	expect(t, strings.Join(got, " ") == "unknown/unknown flag-project/flag-agent two lines/unknown billing/reviewer billing/reviewer", "source = %q; want unknown, then the environment, then the flags", got)
}

func TestRejectedInputIsNotEchoedInFull(t *testing.T) {
	s, _ := notifySandbox(t)
	long := strings.Repeat("x", 200)
	cases := map[string]result{
		"level":        s.do("", "notify", "--level", long, "--title", "t"),
		"channel name": s.do("", "channel", "test", long),
		"body file":    s.do("", "notify", "--level", "note", "--title", "t", "--body-file", "/nonexistent/"+long),
	}
	for name, r := range cases {
		expect(t, r.code != 0 && strings.Contains(r.err, "characters)") && !strings.Contains(r.err, strings.Repeat("x", 70)), "%s: %+v; want the input clipped to 64 characters with its length", name, r)
	}
}

func TestAskAndActionLabelLimits(t *testing.T) {
	s, _ := notifySandbox(t)
	ok := s.do("", "notify", "--level", "note", "--title", "t", "--ask", strings.Repeat("é", 1000), "--action", strings.Repeat("é", 64)+"=https://example.com/a")
	expect(t, ok.code == ExitOK, "at the limits: %+v", ok)
	s.do("", "notify", "--level", "note", "--title", "t", "--ask", strings.Repeat("é", 1001)).fails(t, "long ask", ExitUsage, "1000")
	s.do("", "notify", "--level", "note", "--title", "t", "--action", strings.Repeat("é", 65)+"=https://example.com/a").fails(t, "long label", ExitUsage, "64")
}

package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// --help, -h and help print the usage on stdout and exit 0, wherever an agent tries them: alone, or
// after a command (owner, 2026-10-26). Nothing else runs: no config is read, no secret touched.
func TestHelpPrintsTheUsageAndExits0(t *testing.T) {
	s := newSandbox(t) // no config at all: help needs none
	for _, args := range [][]string{
		{"help"}, {"--help"}, {"-h"},
		{"notify", "--help"}, {"notify", "--level", "page", "-h"}, {"levels", "--help"}, {"flush", "-h"},
		{"status", "--help"}, {"resolve", "--help"}, {"page", "--help"}, {"page", "--low", "-h"}, {"version", "--help"},
		{"config", "--help"}, {"config", "check", "--help"}, {"secret", "set", "--help"}, {"channel", "test", "-h"},
	} {
		r := s.do("", args...)
		expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "usage: yo-human") && r.err == "", "%v: %+v; want the usage on stdout, exit 0", args, r)
	}
	r := s.do("", "--help", "--json")
	var obj struct {
		Status, Usage string
	}
	expect(t, r.code == ExitOK && json.Unmarshal([]byte(r.out), &obj) == nil && obj.Status == "ok" && strings.HasPrefix(obj.Usage, "usage: yo-human"),
		"--help --json: %+v; want one object carrying the usage", r)
}

// A value that happens to be "-h" is a value, not a request for help.
func TestHelpIsNotAFlagsValue(t *testing.T) {
	s, fk := notifySandbox(t)
	r := s.notify("note", "--body", "-h")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT delivered") && len(fk["phone"].Sent) == 1 && fk["phone"].Sent[0].Body == "-h",
		"notify --body -h: %+v; want it sent with the body -h", r)
}

// A command's --help shows that command's own flags (n1).
func TestHelpAfterACommandShowsItsFlags(t *testing.T) {
	s := newSandbox(t)
	for args, want := range map[string]string{"notify --help": "--dedup-key", "notify --level x -h": "--body-file", "resolve -h": "--note", "page --help": "--resolve", "status --help": "--all"} {
		r := s.do("", strings.Fields(args)...)
		expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "usage: yo-human") && strings.Contains(r.out, want), "%s: %+v; want its usage, with %s", args, r, want)
	}
}

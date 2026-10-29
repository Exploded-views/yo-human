package cli

import (
	"strings"
	"testing"
)

// --project and --agent (and YO_HUMAN_PROJECT and YO_HUMAN_AGENT in their place) are at most 64
// characters: they are tags, groups and an incident namespace, not a place for text.
func TestProjectAndAgentAreAtMost64Characters(t *testing.T) {
	s, fk := notifySandbox(t)
	ok, long := strings.Repeat("é", 64), strings.Repeat("é", 65)
	for _, args := range [][]string{
		{"notify", "--level", "page", "--title", "t", "--ask", "a", "--project", long},
		{"notify", "--level", "page", "--title", "t", "--ask", "a", "--agent", long},
		{"resolve", "--dedup-key", "k", "--project", long},
		{"status", "--project", long},
	} {
		r := s.do("", args...)
		r.fails(t, strings.Join(args[:1], " ")+" with 65 characters", ExitUsage, "64 characters")
	}
	for _, env := range []string{"YO_HUMAN_PROJECT", "YO_HUMAN_AGENT"} {
		t.Setenv(env, long)
		s.notify("page").fails(t, env+" with 65 characters", ExitUsage, "64 characters")
		t.Setenv(env, "")
	}
	expect(t, len(fk["pager"].Sent) == 0, "a message went out")
	r := s.do("", "notify", "--level", "page", "--title", "t", "--ask", "a", "--project", ok, "--agent", ok)
	expect(t, r.code == ExitOK, "64 characters: %+v", r)
}

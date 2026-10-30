package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// An agent in one project cannot resolve, or suppress, another project's incident by guessing its
// key: incidents are namespaced by YO_HUMAN_PROJECT, which the harness sets and an agent cannot.
func TestResolveOnlySeesTheCallersProject(t *testing.T) {
	s, fk := notifySandbox(t)
	t.Setenv("YO_HUMAN_PROJECT", "alpha")
	s.notify("page", "--dedup-key", "disk-7").succeeds2(t, "alpha's page")
	t.Setenv("YO_HUMAN_PROJECT", "beta")
	r := s.do("", "resolve", "--dedup-key", "disk-7")
	expect(t, r.code == ExitOK && regexp.MustCompile(`^RESULT noop`).MatchString(r.out), "beta resolving alpha's key: %+v; want noop", r)
	n := len(fk["pager"].Sent)
	r = s.notify("page", "--dedup-key", "disk-7")
	expect(t, regexp.MustCompile(`^RESULT delivered`).MatchString(r.out) && len(fk["pager"].Sent) == n+1, "beta's own page of the same key: %+v; want it sent, not suppressed by alpha's", r)
	t.Setenv("YO_HUMAN_PROJECT", "")
	r = s.do("", "resolve", "--dedup-key", "disk-7", "--project", "alpha") // the human, with no project set
	expect(t, regexp.MustCompile(`^RESULT resolved`).MatchString(r.out), "resolve --project alpha with no YO_HUMAN_PROJECT: %+v", r)
	r = s.do("", "status", "--all")
	mustContain(t, "status after alpha's resolve", r.out, "incidents: 1", "incident disk-7 project=beta")
}

// The namespace is a boundary (fix round 2, M1): with YO_HUMAN_PROJECT set, --project naming
// another project, and status --all, are refused, so an agent cannot close another project's
// incident or cancel the human's queued page for it.
func TestAnotherProjectIsOutOfReachWhenTheProjectIsSet(t *testing.T) {
	s, fk := notifySandbox(t)
	failing(fk["phone"], channel.Transient)
	failing(fk["pager"], channel.Transient)
	t.Setenv("YO_HUMAN_PROJECT", "victim")
	s.notify("page", "--dedup-key", "db-1") // queued
	t.Setenv("YO_HUMAN_PROJECT", "attacker")
	for _, args := range [][]string{
		{"resolve", "--project", "victim", "--dedup-key", "db-1"},
		{"status", "--all"},
		{"status", "--project", "victim"},
		{"notify", "--level", "page", "--title", "t", "--ask", "a", "--project", "victim", "--dedup-key", "db-1"},
	} {
		r := s.do("", args...)
		r.fails(t, strings.Join(args, " "), ExitUsage, "YO_HUMAN_PROJECT")
		mustContain(t, "fix", r.err, "run it from that project")
		mustNotContain(t, "fix", r.err, "unset", "--all", "NONINTERACTIVE")
	}
	s.do("", "resolve", "--project", "attacker", "--dedup-key", "db-1").succeeds2(t, "--project naming its own project")
	t.Setenv("YO_HUMAN_PROJECT", "victim")
	r := s.do("", "status")
	mustContain(t, "victim's status", r.out, "incidents: 1", "incident db-1 project=victim", "queued: 2")
}

// status shows the caller's project's incidents; with no YO_HUMAN_PROJECT (the human at a
// terminal), --project and --all reach any project.
func TestStatusShowsTheCallersProjectUnlessAll(t *testing.T) {
	s, _ := notifySandbox(t)
	s.notify("page", "--dedup-key", "alpha-key", "--project", "alpha")
	s.notify("page", "--dedup-key", "beta-key", "--project", "beta")
	keys := func(args ...string) []string {
		r := s.do("", append([]string{"status", "--json"}, args...)...)
		var v struct {
			Incidents []struct{ Key, Project string }
		}
		expect(t, json.Unmarshal([]byte(r.out), &v) == nil && r.code == ExitOK, "status %v: %+v", args, r)
		var out []string
		for _, i := range v.Incidents {
			out = append(out, i.Project+"/"+i.Key)
		}
		return out
	}
	got := keys("--project", "beta")
	expect(t, len(got) == 1 && got[0] == "beta/beta-key", "status --project beta = %v", got)
	got = keys("--all")
	expect(t, len(got) == 2, "status --all = %v", got)
	t.Setenv("YO_HUMAN_PROJECT", "alpha")
	got = keys()
	expect(t, len(got) == 1 && got[0] == "alpha/alpha-key", "status as alpha = %v", got)
}

// The shim's default key carries a random suffix, so two pages in one second are two incidents,
// and a key cannot be predicted to resolve someone else's page.
func TestPageDefaultKeyHasARandomSuffix(t *testing.T) {
	s, fk := notifySandbox(t)
	write(t, s.userConfig(), notifyConfigBody+"shim:\n  default: { level: page, ask: \"look\" }\n  low: { level: note, ask: \"later\" }\n  agent: orchestrator\n")
	s.do("", "page", "one")
	s.do("", "page", "two")
	sent := fk["pager"].Sent
	expect(t, len(sent) == 2, "pager sends = %d, want 2", len(sent))
	shape := regexp.MustCompile(`^7:unknown/orch-\d{8}T\d{6}Z-[0-9a-f]{4}$`) // as the provider sees it
	for _, m := range sent {
		expect(t, shape.MatchString(m.DedupKey), "default key %q, want orch-<UTC time>-<4 hex>", m.DedupKey)
	}
	expect(t, len(sent) == 2 && sent[0].DedupKey != sent[1].DedupKey, "two pages in the same second share a key")
}

// succeeds2 expects exit 0, whatever the status.
func (r result) succeeds2(t *testing.T, label string) {
	t.Helper()
	expect(t, r.code == ExitOK, "%s: %+v", label, r)
}

// A YO_HUMAN_PROJECT of only blanks names no project, for status --all as for --project.
func TestABlankProjectEnvNamesNoProject(t *testing.T) {
	s, _ := notifySandbox(t)
	t.Setenv("YO_HUMAN_PROJECT", " \t ")
	s.do("", "status", "--all").succeeds2(t, "status --all with a blank YO_HUMAN_PROJECT")
	s.do("", "status", "--project", "other").succeeds2(t, "status --project with a blank YO_HUMAN_PROJECT")
}

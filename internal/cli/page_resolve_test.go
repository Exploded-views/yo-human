package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
)

// passThroughShim maps the shim's two modes onto levels that each reach their own PagerDuty
// instance: default pages the pager, --low pages pager2.
const passThroughShim = `  - name: low-page
    use_when: "low urgency, still an alert"
    send:
      - { channel: phone, interruption: passive }
      - { channel: pager2, interruption: active }
shim:
  default: { level: page, ask: "look now" }
  low:     { level: low-page, ask: "look when you can" }
  agent: orchestrator
`

// passThroughSandbox is a page sandbox with a second PagerDuty instance behind --low.
func passThroughSandbox(t *testing.T) (*sandbox, fakeSet) {
	t.Helper()
	s, fk := notifySandbox(t)
	body := strings.Replace(notifyConfigBody, "  journal: {", "  pager2: { type: pagerduty, routing_key: { secret: pd-low } }\n  journal: {", 1)
	write(t, s.userConfig(), body+passThroughShim)
	s.setSecret("pd-low")
	t.Setenv("YO_HUMAN_AGENT", "")
	t.Setenv("YO_HUMAN_PROJECT", "")
	return s, fk
}

func resolveFails(kind func(error) error) func(context.Context, string, channel.Message) error {
	return func(context.Context, string, channel.Message) error { return kind(errors.New("provider said no")) }
}

// sameKeys reports whether got is exactly want, in order.
func sameKeys(got []string, want ...string) bool { return slices.Equal(got, want) }

func TestPageResolveNeverSeenKeySendsResolve(t *testing.T) {
	s, fk := passThroughSandbox(t)
	r := s.do("", "page", "--resolve", "old-script-key")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "page --resolve: %+v; want resolved, exit 0", r)
	expect(t, sameKeys(fk["pager"].Resolved, "old-script-key"), "pager resolved %v; want the bare key the old script sent", fk["pager"].Resolved)
	expect(t, sameKeys(fk["pager2"].Resolved), "pager2 resolved %v; want nothing: it is the other level's instance", fk["pager2"].Resolved)

	r = s.do("", "page", "--low", "--resolve", "old-script-key")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "page --low --resolve: %+v; want resolved, exit 0", r)
	expect(t, sameKeys(fk["pager2"].Resolved, "old-script-key") && len(fk["pager"].Resolved) == 1,
		"pager2 resolved %v, pager %v; want --low to reach pager2 only", fk["pager2"].Resolved, fk["pager"].Resolved)
	for _, name := range []string{"phone", "journal"} { // ntfy and log cannot resolve: they get nothing, not even a note
		expect(t, len(fk[name].Sent) == 0 && len(fk[name].Resolved) == 0, "%s got sends %d, resolves %d; want nothing", name, len(fk[name].Sent), len(fk[name].Resolved))
	}
	expect(t, s.entries("queue") == 0 && s.entries("dead") == 0, "queue %d, dead %d; want both empty", s.entries("queue"), s.entries("dead"))
}

func TestPageResolveExpiredKeyUsesRecordedInstances(t *testing.T) {
	s, fk := passThroughSandbox(t)
	r := s.do("", "page", "--low", "ci broken", "log", "ci-key")
	expect(t, r.code == ExitOK && len(fk["pager2"].Sent) == 1, "setup: %+v; want --low to page pager2", r)
	s.advance(25 * time.Hour) // past incident_ttl
	mustContain(t, "status", s.do("", "status").out, "incidents: 0")
	r = s.do("", "page", "--resolve", "ci-key") // without --low
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "page --resolve: %+v; want resolved, exit 0", r)
	expect(t, len(fk["pager2"].Resolved) == 1 && fk["pager2"].Resolved[0] == "7:unknown/ci-key",
		"pager2 resolved %v; want the key it was opened with, on the instance it reached", fk["pager2"].Resolved)
	expect(t, len(fk["pager"].Resolved) == 0, "pager resolved %v; want nothing: the default level's instance was never reached", fk["pager"].Resolved)
	phone := fk["phone"].Sent
	expect(t, len(phone) == 2 && phone[1].Resolved, "phone got %+v; want the passive resolve note, as for any resolve", phone)
}

// openUnder pages key under project, then leaves the environment set to project again.
func openUnder(s *sandbox, t *testing.T, key string, projects ...string) {
	t.Helper()
	for _, p := range projects {
		t.Setenv("YO_HUMAN_PROJECT", p)
		s.do("", "page", "db down", "d", key)
	}
}

// With a project set, --resolve never looks at another project: its incident stays open, and the
// resolve takes the pass-through path with the key in this project's namespace.
func TestPageResolveOtherProjectSingleMatchStaysClosedToAProjectCaller(t *testing.T) {
	s, fk := passThroughSandbox(t)
	openUnder(s, t, "shared", "alpha")
	t.Setenv("YO_HUMAN_PROJECT", "beta")
	r := s.do("", "page", "--resolve", "shared")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "page --resolve: %+v; want resolved, exit 0", r)
	expect(t, sameKeys(fk["pager"].Resolved, "4:beta/shared"), "pager resolved %v; want only beta's own key, never alpha's", fk["pager"].Resolved)
	t.Setenv("YO_HUMAN_PROJECT", "alpha")
	mustContain(t, "alpha status", s.do("", "status").out, "incidents: 1")
}

// With no project set, the other project's single incident is closed, as before.
func TestPageResolveOtherProjectSingleMatchWithNoProject(t *testing.T) {
	s, fk := passThroughSandbox(t)
	openUnder(s, t, "shared", "alpha")
	t.Setenv("YO_HUMAN_PROJECT", "")
	r := s.do("", "page", "--resolve", "shared")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "page --resolve: %+v; want resolved, exit 0", r)
	expect(t, sameKeys(fk["pager"].Resolved, "5:alpha/shared"), "pager resolved %v; want alpha's incident closed with alpha's key", fk["pager"].Resolved)
	t.Setenv("YO_HUMAN_PROJECT", "alpha") // status counts the caller's own project
	mustContain(t, "alpha status", s.do("", "status").out, "incidents: 0")
}

func TestPageResolveOtherProjectSeveralMatches(t *testing.T) {
	s, fk := passThroughSandbox(t)
	openUnder(s, t, "shared", "beta", "alpha")
	t.Setenv("YO_HUMAN_PROJECT", "")
	r := s.do("", "page", "--resolve", "shared")
	r.fails(t, "several matches", ExitUsage, "alpha, beta")
	mustContain(t, "stderr", r.err, "YO_HUMAN_PROJECT", "error:", "valid:", "fix:")
	expect(t, len(fk["pager"].Resolved) == 0 && len(fk["phone"].Sent) == 2, "pager resolved %v, phone %d sends; want nothing sent", fk["pager"].Resolved, len(fk["phone"].Sent))
	for _, p := range []string{"alpha", "beta"} {
		t.Setenv("YO_HUMAN_PROJECT", p)
		mustContain(t, p+" status", s.do("", "status").out, "incidents: 1")
	}
}

// A project-scoped caller learns nothing about other projects: not their names, not that they exist.
func TestPageResolveSeveralMatchesNamesNothingToAProjectCaller(t *testing.T) {
	s, fk := passThroughSandbox(t)
	openUnder(s, t, "shared", "beta", "alpha")
	t.Setenv("YO_HUMAN_PROJECT", "gamma")
	r := s.do("", "page", "--resolve", "shared")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "page --resolve: %+v; want the pass-through, exit 0", r)
	mustNotContain(t, "stdout", r.out, "alpha", "beta")
	mustNotContain(t, "stderr", r.err, "alpha", "beta")
	expect(t, sameKeys(fk["pager"].Resolved, "5:gamma/shared"), "pager resolved %v; want gamma's own key", fk["pager"].Resolved)
}

func TestPageResolveNeverSeenKeyCarriesTheProjectsProviderKey(t *testing.T) {
	s, fk := passThroughSandbox(t)
	t.Setenv("YO_HUMAN_PROJECT", "beta")
	r := s.do("", "page", "--resolve", "fresh")
	expect(t, r.code == ExitOK && sameKeys(fk["pager"].Resolved, "4:beta/fresh"), "%+v, pager resolved %v; want the project's provider key", r, fk["pager"].Resolved)
}

func TestPageResolveRefusesProviderFormKeys(t *testing.T) {
	s, fk := passThroughSandbox(t)
	openUnder(s, t, "x", "alpha")
	t.Setenv("YO_HUMAN_PROJECT", "beta")
	for _, key := range []string{"5:alpha/x", "h:" + strings.Repeat("a1", 32)} {
		r := s.do("", "page", "--resolve", key)
		r.fails(t, key, ExitUsage, "internal provider key")
		mustContain(t, "stderr", r.err, "the key the page was opened with")
	}
	expect(t, len(fk["pager"].Resolved) == 0 && len(fk["phone"].Sent) == 1 && s.entries("queue") == 0, "pager resolved %v; want nothing sent", fk["pager"].Resolved)
	t.Setenv("YO_HUMAN_PROJECT", "alpha")
	mustContain(t, "alpha status", s.do("", "status").out, "incidents: 1")
	t.Setenv("YO_HUMAN_PROJECT", "beta")
	for _, key := range []string{"5:ab/x", "abc:def", "h:xyz", "h:" + strings.Repeat("A", 64)} {
		r := s.do("", "page", "--resolve", key)
		expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "%q: %+v; want an ordinary key, resolved", key, r)
	}
}

func TestPageResolvePassThroughTimeoutQueues(t *testing.T) {
	s, fk := passThroughSandbox(t)
	fk["pager"].ResolveFunc = resolveFails(channel.Transient)
	r := s.do("", "page", "--resolve", "slow-key")
	expect(t, r.code == ExitQueued && strings.HasPrefix(r.out, "RESULT queued "), "page --resolve: %+v; want queued, exit 75", r)
	mustContain(t, "stdout", r.out, "next:")
	expect(t, s.entries("queue") == 1, "queue holds %d entries; want the close queued", s.entries("queue"))
	fk["pager"].ResolveFunc = nil
	s.advance(time.Hour)
	s.do("", "flush").succeeds(t, "flush")
	expect(t, s.entries("queue") == 0, "queue holds %d entries after the flush; want 0", s.entries("queue"))
	got := fk["pager"].Resolved
	expect(t, len(got) == 2 && got[1] == "slow-key", "pager resolved %v; want the flush to retry the bare key", got)
}

func TestPageResolvePassThroughPermanentRefusal(t *testing.T) {
	s, fk := passThroughSandbox(t)
	fk["pager"].ResolveFunc = resolveFails(channel.Permanent)
	r := s.do("", "page", "--resolve", "refused-key")
	expect(t, r.code == ExitFailed && strings.HasPrefix(r.out, "RESULT failed "), "page --resolve: %+v; want failed, exit 69", r)
	mustContain(t, "stderr", r.err, "refused-key", "pager")
	j := s.do("", "page", "--resolve", "refused-key2", "--json")
	mustNotContain(t, "json", j.out, "\\u0000", "\x00") // the store's internal key never reaches the output
	expect(t, s.entries("queue") == 0, "queue holds %d entries; a refused close is not retried", s.entries("queue"))
}

func TestPageResolveNoResolveCapableInstance(t *testing.T) {
	s, fk := passThroughSandbox(t)
	body := strings.Replace(passThroughShim, "default: { level: page,", "default: { level: note,", 1)
	body = strings.Replace(body, "low:     { level: low-page,", "low:     { level: note,", 1)
	write(t, s.userConfig(), strings.Replace(notifyConfigBody, "  journal: {", "  pager2: { type: pagerduty, routing_key: { secret: pd-low } }\n  journal: {", 1)+body)
	for _, args := range [][]string{{"page", "--resolve", "k"}, {"page", "--low", "--resolve", "k"}} {
		r := s.do("", args...)
		expect(t, r.code == ExitFailed && strings.HasPrefix(r.out, "RESULT failed "), "%v: %+v; want failed, exit 69, never a silent 0", args, r)
		mustContain(t, "stderr", r.err, "level note", "error:", "valid:", "fix:")
	}
	expect(t, len(fk["phone"].Sent) == 0 && len(fk["journal"].Sent) == 0 && len(fk["pager"].Resolved) == 0 && len(fk["pager2"].Resolved) == 0, "something was sent")
}

func TestResolveCommandUnchangedForUnknownKey(t *testing.T) {
	s, fk := passThroughSandbox(t)
	r := s.do("", "resolve", "--dedup-key", "never-seen")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT noop "), "resolve: %+v; want noop, exit 0", r)
	s.do("", "page", "--low", "ci", "d", "old")
	s.advance(25 * time.Hour)
	r = s.do("", "resolve", "--dedup-key", "old")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT noop "), "resolve of an expired key: %+v; want noop, exit 0", r)
	expect(t, len(fk["pager"].Resolved) == 0 && len(fk["pager2"].Resolved) == 0, "resolved pager %v pager2 %v; want nothing sent", fk["pager"].Resolved, fk["pager2"].Resolved)
}

// A shim level the config does not have is refused when the config is read (78), never a silent success.
func TestPageResolveShimLevelUnknown(t *testing.T) {
	s, fk := passThroughSandbox(t)
	body := strings.Replace(passThroughShim, "default: { level: page,", "default: { level: ghost,", 1)
	write(t, s.userConfig(), strings.Replace(notifyConfigBody, "  journal: {", "  pager2: { type: pagerduty, routing_key: { secret: pd-low } }\n  journal: {", 1)+body)
	r := s.do("", "page", "--resolve", "k")
	expect(t, r.code == ExitConfig && !strings.HasPrefix(r.out, "RESULT resolved") && len(fk["pager"].Resolved) == 0, "page --resolve: %+v; want exit 78 and nothing sent", r)
}

// A closed record that has outlived incident_ttl but was not pruned yet is no record: the key is
// resolved on the shim level's instances, never a silent 0 that sent nothing.
func TestPageResolveClosedRecordPastTTLFallsToRule5(t *testing.T) {
	s, fk := passThroughSandbox(t)
	s.do("", "page", "db down", "d", "k1")
	s.do("", "page", "--resolve", "k1")
	s.advance(25 * time.Hour)
	r := s.do("", "page", "--resolve", "k1")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "page --resolve: %+v; want resolved, exit 0", r)
	expect(t, sameKeys(fk["pager"].Resolved, "7:unknown/k1", "k1"), "pager resolved %v; want the bare key sent the second time", fk["pager"].Resolved)
}

// Re-resolving a key already closed re-sends the idempotent PagerDuty resolve, but tells the human
// nothing more: no passive note to ntfy.
func TestPageResolveClosedRecordSendsNoPassiveNote(t *testing.T) {
	s, fk := passThroughSandbox(t)
	s.do("", "page", "db down", "d", "k2")
	s.do("", "page", "--resolve", "k2")
	for i := 0; i < 2; i++ {
		r := s.do("", "page", "--resolve", "k2")
		expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved "), "repeat %d: %+v; want resolved, exit 0", i, r)
	}
	expect(t, len(fk["pager"].Resolved) == 3, "pager resolved %v; want the resolve each time", fk["pager"].Resolved)
	expect(t, len(fk["phone"].Sent) == 2, "phone got %d sends; want the page and one resolve note only", len(fk["phone"].Sent))
}

// An expired record whose instance has left the config cannot be closed now: the close waits in the
// queue, and the result says so instead of resolved.
func TestPageResolveExpiredRecordOfARemovedInstanceIsQueued(t *testing.T) {
	s, _ := passThroughSandbox(t)
	s.do("", "page", "--low", "ci", "d", "k3")
	s.advance(25 * time.Hour)
	body := strings.Replace(passThroughShim, "{ channel: pager2, interruption: active }", "{ channel: phone, interruption: active }", 1)
	write(t, s.userConfig(), notifyConfigBody+body)
	r := s.do("", "page", "--resolve", "k3")
	expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT queued "), "page --resolve: %+v; want queued beside the config problem (78)", r)
}

// An unrelated instance that cannot be built takes no part in a resolve the record answers: no
// config problem, and no claim of a queued note that does not exist.
func TestPageResolveRecordPathIgnoresAnUnbuildableUnrelatedInstance(t *testing.T) {
	s, fk := passThroughSandbox(t)
	s.do("", "page", "--low", "ci", "d", "k5")
	s.advance(25 * time.Hour)
	removeSecrets(t, s, "ntfy-topic", "pd-low") // pd-critical is gone: the default level's pager, which this key never reached
	r := s.do("", "page", "--resolve", "k5")
	expect(t, r.code == ExitOK && strings.HasPrefix(r.out, "RESULT resolved ") && r.err == "", "page --resolve: %+v; want resolved, exit 0, nothing on stderr", r)
	expect(t, sameKeys(fk["pager2"].Resolved, "7:unknown/k5") && s.entries("queue") == 0, "pager2 resolved %v, queue %d; want the recorded close and an empty queue", fk["pager2"].Resolved, s.entries("queue"))
}

// A closed record whose recorded instance has left the config is not dropped: the close parks, and
// the fallback does not stand in for it.
func TestPageResolveClosedRecordOfARemovedInstanceParks(t *testing.T) {
	s, fk := passThroughSandbox(t)
	s.do("", "page", "--low", "ci", "d", "k6")
	s.do("", "page", "--resolve", "k6")
	body := strings.Replace(passThroughShim, "{ channel: pager2, interruption: active }", "{ channel: phone, interruption: active }", 1)
	write(t, s.userConfig(), notifyConfigBody+body)
	r := s.do("", "page", "--resolve", "k6")
	expect(t, r.code == ExitConfig && strings.HasPrefix(r.out, "RESULT queued "), "page --resolve: %+v; want queued beside the config problem (78)", r)
	expect(t, len(fk["pager"].Resolved) == 0 && s.entries("queue") == 1, "pager resolved %v, queue %d; want the close parked, none sent to the default service", fk["pager"].Resolved, s.entries("queue"))
}

// A closed record that reached only channels that cannot resolve leaves nothing to re-send, so the
// shim level's resolvers get the bare key.
func TestPageResolveClosedRecordOfPassiveChannelsFallsBack(t *testing.T) {
	s, fk := passThroughSandbox(t)
	body := strings.Replace(passThroughShim, "low:     { level: low-page,", "low:     { level: note,", 1)
	write(t, s.userConfig(), strings.Replace(notifyConfigBody, "  journal: {", "  pager2: { type: pagerduty, routing_key: { secret: pd-low } }\n  journal: {", 1)+body)
	s.do("", "page", "--low", "ci", "d", "k7")
	s.do("", "page", "--low", "--resolve", "k7") // closes it: a note level has nothing to resolve, so it fails
	r := s.do("", "page", "--resolve", "k7")
	expect(t, r.code == ExitOK && sameKeys(fk["pager"].Resolved, "k7"), "%+v, pager resolved %v; want the default level's resolver sent the bare key", r, fk["pager"].Resolved)
}

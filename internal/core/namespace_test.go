package core_test

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/core"
)

// sayAs notifies level for project with a dedup key.
func (e *env) sayAs(project, level, key string) core.Result {
	e.t.Helper()
	r, err := e.p.Notify(context.Background(), channel.Message{Level: level, Title: "t", DedupKey: key, Project: project})
	if err != nil {
		e.t.Fatalf("Notify: %v", err)
	}
	return r
}

// One project's dedup key is not another's incident: a repeat is suppressed only within the
// project that opened it, and a resolve closes only its own project's incident. The key a provider
// sees (PagerDuty's dedup_key) is namespaced too: <project>/<key>.
func TestIncidentsAreNamespacedByProject(t *testing.T) {
	caps := map[string]channel.Capabilities{"a": {ReachesHuman: true, Resolve: true, NativeDedup: true}}
	e := newEnv(t, incCfg(0, "a"), caps)
	if r := e.sayAs("alpha", "page", "disk-7"); r.Status != core.StatusDelivered {
		t.Fatalf("alpha: %s", r.Status)
	}
	if r := e.sayAs("beta", "page", "disk-7"); r.Status != core.StatusDelivered {
		t.Fatalf("beta's first page of the same key = %s, want delivered: it is beta's own incident", r.Status)
	}
	if r := e.sayAs("beta", "page", "disk-7"); r.Status != core.StatusSuppressed {
		t.Fatalf("beta's repeat = %s, want suppressed", r.Status)
	}
	wantProviderKeys(t, e.fakes["a"].Sent, "5:alpha/disk-7", "4:beta/disk-7")
	if r := e.resolve(core.IncidentKey("beta", "disk-7"), ""); r.Status != core.StatusResolved {
		t.Fatalf("beta's resolve = %s", r.Status)
	}
	if got := e.fakes["a"].Resolved; len(got) != 1 || got[0] != "4:beta/disk-7" {
		t.Errorf("resolved keys = %v, want [4:beta/disk-7], beta's key only", got)
	}
	if !e.incident(core.IncidentKey("alpha", "disk-7")).Open {
		t.Error("beta's resolve closed alpha's incident")
	}
	if r := e.sayAs("alpha", "page", "disk-7"); r.Status != core.StatusSuppressed {
		t.Errorf("alpha's repeat after beta's resolve = %s, want suppressed", r.Status)
	}
}

// The key of a project's incident is unambiguous: no choice of project and key collides with
// another's.
func TestIncidentKeyIsUnambiguous(t *testing.T) {
	if core.IncidentKey("a/b", "c") == core.IncidentKey("a", "b/c") || core.IncidentKey("a", "b") == core.IncidentKey("", "b") {
		t.Error("two different (project, key) pairs share an incident key")
	}
}

// wantProviderKeys fails unless the sent messages' dedup keys are exactly keys.
func wantProviderKeys(t *testing.T, sent []channel.Message, keys ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, m := range sent {
		seen[m.DedupKey] = true
	}
	for _, k := range keys {
		if !seen[k] {
			t.Errorf("no message reached the channel with dedup key %q; keys seen: %v", k, seen)
		}
	}
	if len(seen) != len(keys) {
		t.Errorf("keys seen %v, want exactly %v", seen, keys)
	}
}

// A provider key over PagerDuty's 255 bytes becomes the project's prefix and a sha256 of the pair.
func TestProviderKeyStaysWithin255Bytes(t *testing.T) {
	long := strings.Repeat("k", 250)
	a, b := core.ProviderKey("alpha", long), core.ProviderKey("beta", long)
	if len(a) > 255 || len(b) > 255 || a == b || !strings.HasPrefix(a, "h:") {
		t.Errorf("ProviderKey = %q, %q; want two different hashed keys (h:...) of at most 255 bytes", a, b)
	}
	if got := core.ProviderKey("alpha", "disk-7"); got != "5:alpha/disk-7" {
		t.Errorf("ProviderKey = %q, want 5:alpha/disk-7", got)
	}
	if got := core.ProviderKey("", "disk-7"); got != "disk-7" {
		t.Errorf("ProviderKey with no project = %q, want the bare key", got)
	}
	if got := core.ProviderKey("alpha", ""); got != "" {
		t.Errorf("ProviderKey with no key = %q, want none: a message with no key is not an incident", got)
	}
}

// Two long projects (64 four-byte runes) that differ only at the end share their kept prefix.
func TestProviderKeyOfALongProject(t *testing.T) {
	// Two long projects (64 four-byte runes) that differ only at the end share their kept prefix;
	// the hash of the whole pair still tells them apart, within 255 bytes and as valid UTF-8.
	p1, p2 := strings.Repeat("\U0001F600", 63)+"a", strings.Repeat("\U0001F600", 63)+"b"
	k1, k2 := core.ProviderKey(p1, "disk-7"), core.ProviderKey(p2, "disk-7")
	if len(k1) > 255 || len(k2) > 255 || k1 == k2 || !utf8.ValidString(k1) {
		t.Errorf("long-project keys %q (%d bytes) and %q (%d bytes); want two different valid keys of at most 255 bytes", k1, len(k1), k2, len(k2))
	}
}

// A project's incident finds its own queued sends: a repeat is suppressed while one is pending, and
// a resolve cancels it.
func TestAProjectsIncidentSeesItsPendingSends(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), map[string]channel.Capabilities{"a": {ReachesHuman: true}})
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	if r := e.sayAs("alpha", "page", "disk-7"); r.Status != core.StatusQueued {
		t.Fatalf("first page = %s, want queued", r.Status)
	}
	if r := e.sayAs("alpha", "page", "disk-7"); r.Status != core.StatusSuppressed {
		t.Errorf("repeat with a send pending = %s, want suppressed", r.Status)
	}
	e.resolve(core.IncidentKey("alpha", "disk-7"), "")
	if q, _, _ := e.counts(); q != 0 {
		t.Errorf("%d entries queued after the resolve, want its send cancelled", q)
	}
}

// A resolve note belongs to its project's incident: once that incident reopens, the note still
// queued for its old generation is dropped, never sent.
func TestAProjectsResolveNoteIsDroppedWhenItsIncidentReopens(t *testing.T) {
	e := newEnv(t, incCfg(0, "a"), map[string]channel.Capabilities{"a": {ReachesHuman: true}})
	e.sayAs("alpha", "page", "disk-7")
	e.fakes["a"].SendFunc = failWith(channel.Transient, "down")
	e.resolve(core.IncidentKey("alpha", "disk-7"), "")
	e.fakes["a"].SendFunc = nil
	e.clk.Advance(2 * time.Minute) // the note's retry is due
	before := len(e.fakes["a"].Sent)
	e.sayAs("alpha", "page", "disk-7") // reopens the incident, then flushes the backlog
	for _, m := range e.fakes["a"].Sent[before:] {
		if m.Resolved {
			t.Errorf("the old generation's resolve note was sent after the incident reopened")
		}
	}
}

// The provider key is injective (controller ruling, round 2): the project is length-prefixed, and a
// hashed key starts with "h:", which no short key (it starts with a digit) can.
func TestProviderKeyIsInjective(t *testing.T) {
	if core.ProviderKey("a/b", "c") == core.ProviderKey("a", "b/c") {
		t.Errorf("project a/b key c and project a key b/c share %q", core.ProviderKey("a", "b/c"))
	}
	hashed := core.ProviderKey("x", strings.Repeat("k", 300))
	for _, try := range []struct{ project, key string }{
		{"x", hashed}, {"h", strings.TrimPrefix(hashed, "h:")},
		{hashed[:max(strings.LastIndex(hashed, "/"), 0)], hashed[strings.LastIndex(hashed, "/")+1:]},
	} {
		if try.project != "" && core.ProviderKey(try.project, try.key) == hashed {
			t.Errorf("project %q key %q, built to look like a hashed key, collides with %q", try.project, try.key, hashed)
		}
	}
	short1, short2 := core.ProviderKey("x", "y"), core.ProviderKey(string([]byte{'x'}), "y")
	if got := core.ProviderKey("caf\u00e9", "k"); got != "5:caf\u00e9/k" {
		t.Errorf("ProviderKey of a non-ASCII project = %q, want the length in bytes, 5:caf\u00e9/k", got)
	}
	if short1 != short2 || short1 != "1:x/y" || core.ProviderKey("x", strings.Repeat("k", 300)) != hashed {
		t.Error("the same project and key give different keys")
	}
}

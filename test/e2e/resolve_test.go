//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestResolveSendsPagerDutyResolveAndPassiveNotes(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	sb.notify("page", "disk-7").want(t, 0, "delivered")
	r := sb.run("resolve", "--dedup-key", "disk-7", "--note", "Freed 40 GB")
	r.want(t, 0, "resolved")

	pd := sb.pd.requests()
	if len(pd) != 2 {
		t.Fatalf("pagerduty requests: %v", summarize(pd))
	}
	expect(t, "the resolve carries the trigger's dedup key", pd[1].obj(t),
		map[string]any{"event_action": "resolve", "dedup_key": "4:proj/disk-7", "routing_key": secretValues["pd-key"]})
	// ntfy and Bark have no incidents: each gets a passive note instead.
	nt := sb.ntfy.requests()
	bk := sb.bark.requests()
	if len(nt) != 2 || len(bk) != 2 {
		t.Fatalf("ntfy %v bark %v", summarize(nt), summarize(bk))
	}
	nn, bn := nt[1].obj(t), bk[1].obj(t)
	expect(t, "ntfy note", nn, map[string]any{"priority": float64(2)})
	expect(t, "bark note", bn, map[string]any{"level": "passive"})
	for _, note := range []map[string]any{nn, bn} {
		if !strings.HasPrefix(note["title"].(string), "Resolved: ") {
			t.Errorf("a resolve note is titled Resolved: ...: %v", note["title"])
		}
	}
	if !strings.Contains(nn["message"].(string), "Freed 40 GB") {
		t.Errorf("the note text reaches ntfy: %v", nn["message"])
	}
	if v := sb.status(); len(v.Incidents) != 0 {
		t.Errorf("incident still open: %+v", v.Incidents)
	}
	// A second resolve has nothing to close and sends nothing.
	sb.run("resolve", "--dedup-key", "disk-7").want(t, 0, "noop")
	sb.pd.wantCount(t, 2)
	sb.ntfy.wantCount(t, 2)
	sb.bark.wantCount(t, 2)
}

// A resolve note goes only to a channel that delivered; PagerDuty never triggered, so it hears nothing.
func TestResolveNotesGoOnlyToChannelsThatDelivered(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	sb.pd.down(400)
	sb.notify("page", "disk-7").want(t, 0, "partial")
	sb.run("resolve", "--dedup-key", "disk-7").want(t, 0, "resolved")
	sb.pd.wantCount(t, 1) // the failed trigger only; no resolve
	sb.ntfy.wantCount(t, 2)
	sb.bark.wantCount(t, 2)
}

func TestResolveCancelsHeldPagesLocallyAndAtNtfy(t *testing.T) {
	sb, end := quietSandbox(t)
	id := sb.notify("page", "disk-7").want(t, 0, "held")
	r := sb.run("resolve", "--dedup-key", "disk-7")
	r.want(t, 0, "resolved")
	if !r.hasLine("bell: cancelled") || !r.hasLine("pager: cancelled") {
		t.Errorf("held entries should be cancelled:\n%s", r.out)
	}
	nt := sb.ntfy.requests()
	if len(nt) < 2 || nt[1].Method != "DELETE" || nt[1].Path != "/"+secretValues["ntfy-topic"]+"/"+id {
		t.Fatalf("ntfy must cancel the scheduled page with DELETE /<topic>/<sequence_id>: %v", summarize(nt))
	}
	if nt[1].Auth != "Bearer "+secretValues["ntfy-token"] {
		t.Error("the cancel carries the bearer token")
	}
	sb.bark.wantCount(t, 0)
	sb.pd.wantCount(t, 0)
	if v := sb.status(); len(v.Held)+len(v.Queued)+len(v.Dead)+len(v.Incidents) != 0 {
		t.Errorf("nothing should remain: %+v", v)
	}
	sb.at = end.Add(time.Minute)
	sb.run("flush").want(t, 0, "ok")
	sb.bark.wantCount(t, 0)
	sb.pd.wantCount(t, 0)
}

// ntfy answers 404 to a cancel: it may be gone, or the server may be too old. That is a permanent
// failure the human must see, because the page might still fire.
func TestResolveCancelAnswered404IsPermanent(t *testing.T) {
	sb, _ := quietSandbox(t)
	sb.notify("page", "disk-7").want(t, 0, "held")
	sb.ntfy.dcode.Store(404)
	r := sb.run("resolve", "--dedup-key", "disk-7")
	r.want(t, 69, "resolved")
	if !strings.Contains(r.errOut, "may still be paged") {
		t.Errorf("stderr should warn that the page may still fire:\n%s", r.errOut)
	}
	if got := len(sb.ntfy.requests()); got != 2 {
		t.Errorf("a permanent cancel failure is not retried: %d ntfy requests", got)
	}
}

// ntfy cannot be reached for the cancel: the resolve leaves it queued, exits 75 with a next step
// (spec amendment 8), and the flush it names retries it.
func TestResolveCancelFailingTransientlyIsRetriedByFlush(t *testing.T) {
	sb, _ := quietSandbox(t)
	id := sb.notify("page", "disk-7").want(t, 0, "held")
	sb.ntfy.dcode.Store(503)
	r := sb.run("resolve", "--dedup-key", "disk-7")
	r.want(t, 75, "resolved")
	if !strings.Contains(r.out, "\nnext: yo-human flush in ") {
		t.Errorf("a queued cancel needs a next step:\n%s", r.out)
	}
	v := sb.status()
	if len(v.Queued) != 1 || v.Queued[0].Kind != "cancel" || v.Queued[0].Channel != "phone" {
		t.Fatalf("the cancel should wait in the queue: %+v", v.Queued)
	}
	sb.ntfy.dcode.Store(0)
	sb.advance(2 * time.Minute)
	sb.run("flush").want(t, 0, "ok")
	last := sb.ntfy.requests()
	if got := last[len(last)-1]; got.Method != "DELETE" || !strings.HasSuffix(got.Path, "/"+id) {
		t.Errorf("last ntfy request: %s %s", got.Method, got.Path)
	}
}

// PagerDuty is down when the incident is resolved: its alert stays open and keeps escalating until
// the close gets through. The resolve exits 75 with a next step, and the flush closes the alert.
func TestResolveWhilePagerDutyIsDownSaysToFlush(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	sb.notify("page", "disk-7").want(t, 0, "delivered")
	sb.pd.down(503)
	r := sb.run("resolve", "--dedup-key", "disk-7", "--json")
	r.wantExit(t, 75)
	var obj struct {
		Status string
		Next   *struct{ Do string }
		Errors []struct{ What string }
	}
	must(t, json.Unmarshal([]byte(r.out), &obj))
	if obj.Status != "resolved" || obj.Next == nil || obj.Next.Do != "yo-human flush" || len(obj.Errors) != 1 || !strings.Contains(obj.Errors[0].What, "pager") {
		t.Fatalf("resolve --json: %s; want resolved, a flush as next, and the close of pager as the error", r.out)
	}
	sb.pd.up()
	sb.advance(2 * time.Minute)
	sb.run("flush").want(t, 0, "ok")
	expect(t, "the close that got through", sb.pd.requests()[2].obj(t), map[string]any{"event_action": "resolve", "dedup_key": "4:proj/disk-7"})
}

// An incident that expired while its page is held for quiet hours: the resolve reports noop, yet
// still stops the held page, at ntfy and in the queue, so nothing fires when the window ends.
func TestResolvingAnExpiredHeldPageStopsIt(t *testing.T) {
	sb := newSandbox(t)
	block, end := quietBlock(sb.at)
	sb.writeConfig(strings.Replace(baseConfig, "deadline: 10s\n", "deadline: 10s\nincident_ttl: 30m\n", 1) + block)
	id := sb.notify("page", "disk-7").want(t, 0, "held")
	sb.advance(40 * time.Minute)
	sb.run("resolve", "--dedup-key", "disk-7").want(t, 0, "noop")
	if nt := sb.ntfy.requests(); len(nt) != 2 || nt[1].Method != "DELETE" || !strings.HasSuffix(nt[1].Path, "/"+id) {
		t.Errorf("ntfy got %v; want the held page deleted", summarize(nt))
	}
	sb.at = end.Add(time.Minute)
	sb.run("flush").want(t, 0, "ok")
	sb.bark.wantCount(t, 0)
	sb.pd.wantCount(t, 0)
}

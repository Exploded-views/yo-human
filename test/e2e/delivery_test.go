//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDeliveredOnAllThree(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	r := sb.run("notify", "--level", "page", "--title", "Disk is full on build-7", "--ask", "Free some space",
		"--body", "df says 100%", "--link", "https://example.com/runbook", "--dedup-key", "disk-7", "--project", "proj", "--agent", "tester", "--json")
	r.wantExit(t, 0)
	id := checkDeliveredJSON(t, r)

	checkNtfyPage(t, sb.ntfy.only(t), id)
	checkBarkPage(t, sb.bark.only(t))
	checkPagerDutyPage(t, sb.pd.only(t))

	v := sb.status()
	if len(v.Queued)+len(v.Held)+len(v.Dead) != 0 || len(v.Incidents) != 1 || v.Incidents[0].Key != "disk-7" || v.Incidents[0].Level != "page" {
		t.Errorf("status after delivery: %+v", v)
	}
}

func TestPartialWhenPagerDutyReturns503(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	sb.pd.down(503)
	r := sb.notify("page", "disk-7")
	r.want(t, 0, "partial")
	if !r.hasLine("pager: queued (pagerduty answered HTTP 503)") || !r.hasLine("phone: sent") || !r.hasLine("bell: sent") {
		t.Errorf("channel lines:\n%s", r.out)
	}
	sb.ntfy.wantCount(t, 1)
	sb.bark.wantCount(t, 1)
	sb.pd.wantCount(t, 1)
	v := sb.status()
	if len(v.Queued) != 1 || v.Queued[0].Channel != "pager" || v.Queued[0].Kind != "send" || v.Queued[0].Attempts != 1 {
		t.Errorf("status: %+v", v.Queued)
	}
	sb.pd.wantCount(t, 1) // status found nothing due yet (the retry is a minute out)
}

func TestQueuedWhenEverythingIsDownThenFlushDelivers(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	for _, f := range sb.fakesAll() {
		f.down(503)
	}
	r := sb.notify("page", "disk-7")
	id := r.want(t, 75, "queued")
	if v := sb.status(); len(v.Queued) != 3 || !strings.Contains(r.out, "\nnext: ") {
		t.Fatalf("want 3 queued entries and a next step:\n%s%+v", r.out, v.Queued)
	}
	for _, f := range sb.fakesAll() {
		f.up()
	}
	// Not due yet: the retry is 60 seconds after the failed attempt.
	sb.run("flush").want(t, 0, "ok")
	for _, f := range sb.fakesAll() {
		f.wantCount(t, 1)
	}
	sb.advance(2 * time.Minute)
	fl := sb.run("flush")
	fl.want(t, 0, "ok")
	for _, f := range sb.fakesAll() {
		f.wantCount(t, 2)
	}
	expect(t, "the retried page", sb.pd.requests()[1].obj(t), map[string]any{"dedup_key": "4:proj/disk-7", "event_action": "trigger"})
	expect(t, "the retried ntfy message keeps its id", sb.ntfy.requests()[1].obj(t), map[string]any{"sequence_id": id})
	if v := sb.status(); len(v.Queued)+len(v.Dead) != 0 {
		t.Errorf("queue not empty after the flush: %+v", v)
	}
}

func TestFailedWhenEverythingReturns400(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	for _, f := range sb.fakesAll() {
		f.down(400)
	}
	r := sb.notify("page", "disk-7")
	r.want(t, 69, "failed")
	if r.hasLine("next:") || strings.Contains(r.out, "next:") {
		t.Errorf("a failed result has no next step:\n%s", r.out)
	}
	wantLines(t, r, "phone: dead (ntfy answered HTTP 400)", "bell: dead (bark answered HTTP 400)", "pager: dead (pagerduty answered HTTP 400)")
	v := sb.status()
	if len(v.Dead) != 3 || len(v.Queued) != 0 {
		t.Errorf("want 3 dead, none queued: %+v", v)
	}
	checkDeadReasons(t, v, map[string]string{"phone": "ntfy answered HTTP 400", "bell": "bark answered HTTP 400", "pager": "pagerduty answered HTTP 400"})
	for _, f := range sb.fakesAll() {
		f.wantCount(t, 1) // permanent: never retried
	}
	sb.advance(time.Hour)
	sb.run("flush").want(t, 0, "ok")
	for _, f := range sb.fakesAll() {
		f.wantCount(t, 1)
	}
}

// A repeat of an incident that reached no human, with nothing pending, is sent again.
func TestRepeatOfAnIncidentThatReachedNoOneIsSent(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	for _, f := range sb.fakesAll() {
		f.down(400)
	}
	sb.notify("page", "disk-7").want(t, 69, "failed")
	for _, f := range sb.fakesAll() {
		f.up()
	}
	sb.notify("page", "disk-7").want(t, 0, "delivered")
	for _, f := range sb.fakesAll() {
		f.wantCount(t, 2)
	}
}

// The proxy variables point at a closed port. A channel that falls back to the real Bark server
// must fail loudly (queued, unreachable) and send nothing anywhere.
func TestNoRealNetworkGuard(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(`version: 1
deadline: 10s
channels:
  bell:
    type: bark
    device_key: { secret: bark-key }
levels:
  - name: note
    use_when: "FYI."
    ask: optional
    send:
      - { channel: bell, interruption: passive }
`)
	r := sb.run("notify", "--level", "note", "--title", "guard")
	r.want(t, 75, "queued")
	if !r.hasLine("bell: queued (bark server could not be reached)") {
		t.Errorf("the stray call must fail visibly:\n%s", r.out)
	}
	sb.bark.wantCount(t, 0)
}

func checkNtfyPage(t *testing.T, n rec, id string) {
	t.Helper()
	if n.Method != "POST" || n.Path != "/" {
		t.Errorf("ntfy request: %s %s", n.Method, n.Path)
	}
	if n.Auth != "Bearer "+secretValues["ntfy-token"] {
		t.Error("the ntfy request must carry the configured bearer token")
	}
	nb := n.obj(t)
	expect(t, "ntfy body", nb, map[string]any{"topic": secretValues["ntfy-topic"], "title": "Disk is full on build-7",
		"priority": float64(3), "sequence_id": id, "click": "https://example.com/runbook", "delay": nil})
	for _, part := range []string{"Free some space", "df says 100%"} {
		if !strings.Contains(nb["message"].(string), part) {
			t.Errorf("ntfy message lacks %q: %v", part, nb["message"])
		}
	}

}

func checkBarkPage(t *testing.T, b rec) {
	t.Helper()
	if b.Method != "POST" || b.Path != "/push" {
		t.Errorf("bark request: %s %s", b.Method, b.Path)
	}
	expect(t, "bark body", b.obj(t), map[string]any{"device_key": secretValues["bark-key"], "level": "timeSensitive",
		"title": "Disk is full on build-7", "url": "https://example.com/runbook"})

}

func checkPagerDutyPage(t *testing.T, p rec) {
	t.Helper()
	if p.Method != "POST" || p.Path != "/v2/enqueue" {
		t.Errorf("pagerduty request: %s %s", p.Method, p.Path)
	}
	pb := p.obj(t)
	expect(t, "pagerduty event", pb, map[string]any{"routing_key": secretValues["pd-key"], "event_action": "trigger", "dedup_key": "4:proj/disk-7"})
	expect(t, "pagerduty payload", sub(pb, "payload"), map[string]any{"severity": "error", "summary": "Disk is full on build-7", "source": "proj"})

}

// checkDeliveredJSON checks the --json result object of a page sent on all three channels and
// returns its id.
func checkDeliveredJSON(t *testing.T, r result) string {
	t.Helper()
	var res struct {
		Version  int    `json:"version"`
		Status   string `json:"status"`
		ID       string `json:"id"`
		Channels []struct{ Channel, Status string }
	}
	must(t, json.Unmarshal([]byte(r.out), &res))
	if res.Version != 1 || res.Status != "delivered" || res.ID == "" || len(res.Channels) != 3 {
		t.Fatalf("unexpected result object: %s", r.out)
	}
	for _, c := range res.Channels {
		if c.Status != "sent" {
			t.Errorf("channel %s: %s, want sent", c.Channel, c.Status)
		}
	}
	return res.ID
}

// PagerDuty's secret is gone (a rotation half done). ntfy and Bark still page the human; the call
// exits 78 and names the missing secret, so the human hears of it.
func TestOneMissingSecretDoesNotSilenceTheLevel(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	must(t, os.WriteFile(sb.secretsPath(), []byte(fmt.Sprintf("ntfy-topic: %s\nntfy-token: %s\nbark-key: %s\n", secretValues["ntfy-topic"], secretValues["ntfy-token"], secretValues["bark-key"])), 0o600))
	r := sb.notify("urgent", "disk-7")
	r.want(t, 78, "partial")
	wantLines(t, r, "phone: sent", "bell: sent")
	if !strings.Contains(r.out, "pager: skipped") || !strings.Contains(r.errOut, `"pd-key"`) {
		t.Errorf("want pager skipped and the missing secret named:\n%s%s", r.out, r.errOut)
	}
	sb.ntfy.wantCount(t, 1)
	sb.bark.wantCount(t, 1)
	sb.pd.wantCount(t, 0)
}

// checkDeadReasons fails unless each dead entry is a send whose reason is its channel's in want.
func checkDeadReasons(t *testing.T, v view, want map[string]string) {
	t.Helper()
	for _, d := range v.Dead {
		if d.Kind != "send" || d.Reason != want[d.Channel] {
			t.Errorf("dead entry %+v; want the send with the provider's answer as its reason", d)
		}
	}
}

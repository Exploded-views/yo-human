//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// quietSandbox is a sandbox whose clock is inside a quiet-hours window; end is when it ends.
func quietSandbox(t *testing.T) (*sandbox, time.Time) {
	t.Helper()
	sb := newSandbox(t)
	block, end := quietBlock(sb.at)
	sb.writeConfig(baseConfig + block)
	return sb, end
}

func TestHeldInQuietHoursThenFlushAfterTheWindow(t *testing.T) {
	sb, end := quietSandbox(t)
	r := sb.notify("page", "disk-7")
	id := r.want(t, 0, "held")
	// ntfy schedules the page itself (the provider holds it); Bark and PagerDuty cannot, so the queue holds theirs.
	wantLines(t, r, "phone: held", "bell: held", "pager: held")
	expect(t, "ntfy schedules for the window's end", sb.ntfy.only(t).obj(t),
		map[string]any{"delay": strconv.FormatInt(end.Unix(), 10), "sequence_id": id})
	sb.bark.wantCount(t, 0)
	sb.pd.wantCount(t, 0)
	v := sb.status()
	if len(v.Held) != 2 {
		t.Fatalf("want two entries held: %+v", v.Held)
	}
	for _, h := range v.Held {
		if !h.DeliverAt.Equal(end) {
			t.Errorf("%s is held until %s, want %s", h.Channel, h.DeliverAt, end)
		}
	}

	sb.advance(time.Hour) // still inside the window
	sb.run("flush").want(t, 0, "ok")
	sb.bark.wantCount(t, 0)
	sb.pd.wantCount(t, 0)

	sb.at = end.Add(time.Minute)
	sb.run("flush").want(t, 0, "ok")
	expect(t, "released bark send", sb.bark.only(t).obj(t), map[string]any{"level": "timeSensitive"})
	expect(t, "released pagerduty send", sb.pd.only(t).obj(t), map[string]any{"event_action": "trigger", "dedup_key": "4:proj/disk-7"})
	sb.ntfy.wantCount(t, 1) // the provider holds ntfy's; the flush sends nothing more
	if v := sb.status(); len(v.Held)+len(v.Queued)+len(v.Dead) != 0 {
		t.Errorf("queue not empty: %+v", v)
	}
}

func TestUrgentIgnoresQuietHours(t *testing.T) {
	sb, _ := quietSandbox(t)
	sb.notify("urgent", "disk-7").want(t, 0, "delivered")
	sb.pd.wantCount(t, 1)
}

func TestRepeatSuppressedAndLouderEscalates(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	sb.notify("page", "disk-7").want(t, 0, "delivered")
	// The same page again, and a quieter one: recorded, nothing sent.
	sb.notify("page", "disk-7").want(t, 0, "suppressed")
	sb.notify("note", "disk-7").want(t, 0, "suppressed")
	for _, f := range sb.fakesAll() {
		f.wantCount(t, 1)
	}
	// A louder level raises the incident and goes to every channel of the new level.
	sb.notify("urgent", "disk-7").want(t, 0, "delivered")
	for _, f := range sb.fakesAll() {
		f.wantCount(t, 2)
	}
	pd := sb.pd.requests()[1].obj(t)
	expect(t, "escalation to PagerDuty", pd, map[string]any{"dedup_key": "4:proj/disk-7"})
	expect(t, "escalation to PagerDuty", sub(pd, "payload"), map[string]any{"severity": "critical"})
	expect(t, "escalation to Bark", sb.bark.requests()[1].obj(t), map[string]any{"level": "critical"})
	if v := sb.status(); len(v.Incidents) != 1 || v.Incidents[0].Level != "urgent" {
		t.Errorf("incident should now be at urgent: %+v", v.Incidents)
	}
}

// A louder repeat during quiet hours pages now. The quieter copy it replaces must not fire when the
// window ends: ntfy's scheduled message is cancelled, and the copies held for Bark and PagerDuty
// leave the queue.
func TestALouderRepeatInQuietHoursPagesOnce(t *testing.T) {
	sb, end := quietSandbox(t)
	id := sb.notify("page", "disk-7").want(t, 0, "held")
	sb.notify("urgent", "disk-7").want(t, 0, "delivered")
	if got := sb.ntfy.requests(); len(got) != 3 || got[2].Method != "DELETE" || !strings.HasSuffix(got[2].Path, "/"+id) {
		t.Errorf("ntfy got %v; want the scheduled page, the urgent one, then the scheduled page deleted", summarize(got))
	}
	if v := sb.status(); len(v.Held)+len(v.Queued)+len(v.Dead) != 0 {
		t.Errorf("queue after the louder repeat: %+v; want the held copies gone", v)
	}
	sb.at = end.Add(time.Minute)
	sb.run("flush").want(t, 0, "ok")
	sb.bark.wantCount(t, 1)
	sb.pd.wantCount(t, 1)
	expect(t, "the one PagerDuty page", sb.pd.only(t).obj(t), map[string]any{"event_action": "trigger"})
	expect(t, "the one Bark push", sb.bark.only(t).obj(t), map[string]any{"level": "critical"})
}

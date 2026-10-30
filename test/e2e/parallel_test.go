//go:build e2e

package e2e

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// Two real processes notify with the same dedup key at the same moment. The providers answer slowly,
// so the second process arrives while the first is still sending. Exactly one pages.
func TestParallelNotifiesWithTheSameKeyPageOnce(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(baseConfig)
	for _, f := range []*fake{sb.ntfy, sb.bark, sb.pd} {
		f.delay.Store(int64(200 * time.Millisecond))
	}
	const rounds = 4
	for i := 0; i < rounds; i++ {
		key := fmt.Sprintf("disk-%d", i)
		args := []string{"notify", "--level", "page", "--title", "Disk is full", "--ask", "Free some space", "--dedup-key", key}
		a, b := sb.start(args...), sb.start(args...)
		ra, rb := a.wait(), b.wait()
		sa, _ := ra.head(t)
		sbs, _ := rb.head(t)
		ra.wantExit(t, 0)
		rb.wantExit(t, 0)
		got := []string{sa, sbs}
		onePaged := slices.Contains(got, "delivered") && slices.Contains(got, "suppressed")
		if !onePaged {
			t.Fatalf("round %d: statuses %s and %s, want one delivered and one suppressed", i, sa, sbs)
		}
		for _, f := range []*fake{sb.ntfy, sb.bark, sb.pd} {
			f.wantCount(t, i+1)
		}
	}
	if v := sb.status(); len(v.Incidents) != rounds || len(v.Queued)+len(v.Dead) != 0 {
		t.Errorf("status: %+v", v)
	}
}

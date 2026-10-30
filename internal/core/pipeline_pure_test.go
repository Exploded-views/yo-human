package core_test

import (
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
)

func TestNextAttemptCurve(t *testing.T) {
	r := config.Retry{Every: 60 * time.Second, Times: 20}
	tests := []struct {
		attempts int
		want     time.Duration
		ok       bool
	}{
		{0, 60 * time.Second, true},
		{1, 120 * time.Second, true},
		{3, 480 * time.Second, true},
		{10, time.Hour, true},
		{19, time.Hour, true},
		{20, 0, false},
		{21, 0, false},
	}
	for _, tc := range tests {
		got, ok := core.NextAttempt(r, tc.attempts, t0)
		if ok != tc.ok || (ok && !got.Equal(t0.Add(tc.want))) {
			t.Errorf("attempts %d: %v, %v; want +%v, %v", tc.attempts, got.Sub(t0), ok, tc.want, tc.ok)
		}
	}
	if _, ok := core.NextAttempt(config.Retry{Every: time.Minute, Times: 0}, 0, t0); ok {
		t.Error("times 0 allowed a retry")
	}
	if got, ok := core.NextAttempt(config.Retry{Every: time.Minute, Times: 1 << 30}, 1<<29, t0); !ok || !got.Equal(t0.Add(time.Hour)) {
		t.Errorf("huge attempts = %v, %v; want the cap, without overflow", got.Sub(t0), ok)
	}
}

func TestSummarize(t *testing.T) {
	rh := map[string]bool{"a": true, "b": true, "file": false}
	cr := func(kv ...string) []core.ChannelResult {
		var out []core.ChannelResult
		for i := 0; i < len(kv); i += 2 {
			out = append(out, core.ChannelResult{Channel: kv[i], Status: kv[i+1]})
		}
		return out
	}
	tests := []struct {
		name string
		in   []core.ChannelResult
		want core.Status
	}{
		{"all sent", cr("a", "sent", "b", "sent"), core.StatusDelivered},
		{"sent and queued", cr("a", "sent", "b", "queued"), core.StatusPartial},
		{"sent and dead", cr("a", "sent", "b", "dead"), core.StatusPartial},
		{"sent and held", cr("a", "sent", "b", "held"), core.StatusPartial},
		{"queued only", cr("a", "queued", "b", "dead"), core.StatusQueued},
		{"queued and held", cr("a", "queued", "b", "held"), core.StatusQueued},
		{"held only", cr("a", "held", "b", "held"), core.StatusHeld},
		{"all dead", cr("a", "dead", "b", "dead"), core.StatusFailed},
		{"log sent, human queued", cr("file", "sent", "a", "queued"), core.StatusQueued},
		{"log sent, human dead", cr("file", "sent", "a", "dead"), core.StatusFailed},
		{"log only sent", cr("file", "sent"), core.StatusFailed},
		{"sent and skipped", cr("a", "sent", "b", "skipped"), core.StatusPartial},
		{"skipped only is still on its way", cr("a", "skipped", "b", "skipped"), core.StatusQueued},
		{"skipped and dead is not failed", cr("a", "skipped", "b", "dead"), core.StatusQueued},
		{"skipped and held", cr("a", "skipped", "b", "held"), core.StatusQueued},
		{"nothing", nil, core.StatusFailed},
	}
	for _, tc := range tests {
		if got := core.Summarize(tc.in, rh); got != tc.want {
			t.Errorf("%s: %q; want %q", tc.name, got, tc.want)
		}
	}
}

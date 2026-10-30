package core_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/channel"
	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
	"github.com/Exploded-views/yo-human/internal/problem"
)

func testConfig(loc *time.Location) *config.Config {
	return &config.Config{
		Levels: []config.Level{
			{Name: "log", UseWhen: "nothing needs a human", Send: []config.Send{{Channel: "file", Interruption: channel.Passive}}},
			{Name: "nudge", UseWhen: "a human should look today", HoldInQuietHours: true, Send: []config.Send{
				{Channel: "phone", Interruption: channel.Active, Options: map[string]any{"tag": "a"}},
				{Channel: "pager", Interruption: channel.TimeSensitive, Options: map[string]any{"k": 1}},
				{Channel: "file", Interruption: channel.Passive},
			}},
			{Name: "page", UseWhen: "wake the human", Send: []config.Send{{Channel: "pager", Interruption: channel.Critical}}},
		},
		QuietHours: config.QuietHours{Location: loc, Windows: []config.Window{{From: 22 * 60, To: 7 * 60}}},
	}
}

func TestPlanFansOutEverySend(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, loc)
	got, err := core.Plan(testConfig(loc), "nudge", now, nil)
	if err != nil || len(got) != 3 {
		t.Fatalf("Plan = %v, %v; want 3 sends", got, err)
	}
	want := []core.PlannedSend{
		{Instance: "phone", Interruption: channel.Active, Options: map[string]any{"tag": "a"}},
		{Instance: "pager", Interruption: channel.TimeSensitive, Options: map[string]any{"k": 1}},
		{Instance: "file", Interruption: channel.Passive},
	}
	for i, w := range want {
		if !reflect.DeepEqual(got[i], w) {
			t.Errorf("send %d = %+v; want %+v (outside quiet hours, so not deferred)", i, got[i], w)
		}
	}
}

func TestPlanUnknownLevelListsLevels(t *testing.T) {
	loc := chicago(t)
	_, err := core.Plan(testConfig(loc), "urgent", time.Date(2026, 10, 30, 12, 0, 0, 0, loc), nil)
	p, ok := problem.As(err)
	if !ok || p.Kind != problem.Usage {
		t.Fatalf("err = %v; want a usage problem", err)
	}
	if !strings.Contains(p.What, `"urgent"`) {
		t.Errorf("what = %q; want it to name the level", p.What)
	}
	want := "levels, quietest first: log (use when nothing needs a human), nudge (use when a human should look today), page (use when wake the human)"
	if p.Valid != want {
		t.Errorf("valid = %q\nwant    %q", p.Valid, want)
	}
	if p.Fix != `retry with --level <name>; run "yo-human levels --json" to read them` {
		t.Errorf("fix = %q", p.Fix)
	}
	var target *problem.Problem
	if !errors.As(err, &target) {
		t.Error("errors.As does not find the problem")
	}
}

func TestPlanHoldInQuietHours(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 10, 30, 23, 0, 0, 0, loc)
	end := time.Date(2026, 10, 31, 7, 0, 0, 0, loc)
	caps := map[string]channel.Capabilities{"phone": {Schedule: true}, "pager": {}}
	got, err := core.Plan(testConfig(loc), "nudge", now, caps)
	if err != nil || len(got) != 3 {
		t.Fatalf("Plan = %v, %v", got, err)
	}
	if !got[0].DeliverAt.Equal(end) || got[0].HoldLocally {
		t.Errorf("scheduling channel = %+v; want DeliverAt %v and no local hold", got[0], end)
	}
	if !got[1].DeliverAt.Equal(end) || !got[1].HoldLocally {
		t.Errorf("non-scheduling channel = %+v; want DeliverAt %v and a local hold", got[1], end)
	}
	if !got[2].DeliverAt.Equal(end) || !got[2].HoldLocally {
		t.Errorf("channel with no capabilities entry = %+v; want a local hold", got[2])
	}
}

func TestPlanSendLevelIgnoresQuietHours(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 10, 30, 23, 0, 0, 0, loc)
	got, err := core.Plan(testConfig(loc), "page", now, map[string]channel.Capabilities{"pager": {Schedule: true}})
	if err != nil || len(got) != 1 {
		t.Fatalf("Plan = %v, %v", got, err)
	}
	if !got[0].DeliverAt.IsZero() || got[0].HoldLocally {
		t.Errorf("a send level was deferred: %+v", got[0])
	}
}

func TestPlanHoldLevelOutsideWindowIsNotDeferred(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 10, 30, 7, 0, 0, 0, loc)
	got, _ := core.Plan(testConfig(loc), "nudge", now, nil)
	for i, p := range got {
		if !p.DeliverAt.IsZero() || p.HoldLocally {
			t.Errorf("send %d at the window's end was deferred: %+v", i, p)
		}
	}
}

func TestPlanOptionsAreCopied(t *testing.T) {
	loc := chicago(t)
	cfg := testConfig(loc)
	got, _ := core.Plan(cfg, "nudge", time.Date(2026, 10, 30, 12, 0, 0, 0, loc), nil)
	got[0].Options["tag"] = "changed"
	got[0].Options["extra"] = true
	if cfg.Levels[1].Send[0].Options["tag"] != "a" || len(cfg.Levels[1].Send[0].Options) != 1 {
		t.Errorf("mutating a planned send changed the config: %v", cfg.Levels[1].Send[0].Options)
	}
}

func TestPlanUnknownLevelEchoesAtMostSixtyFourCharacters(t *testing.T) {
	loc := chicago(t)
	_, err := core.Plan(testConfig(loc), strings.Repeat("x", 200), time.Date(2026, 10, 30, 12, 0, 0, 0, loc), nil)
	p, _ := problem.As(err)
	if p == nil || !strings.Contains(p.What, "(200 characters)") || strings.Contains(p.What, strings.Repeat("x", 65)) {
		t.Errorf("what = %v; want the level clipped to 64 characters with its length", p)
	}
}

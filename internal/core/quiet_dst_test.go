package core_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load zone %s: %v", name, err)
	}
	return loc
}

func utc(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, time.UTC)
}

func oneWindow(loc *time.Location, from, to int) config.QuietHours {
	var w config.Window
	// config.clock is unexported, so a variable cannot be assigned to it directly.
	reflect.ValueOf(&w).Elem().FieldByName("From").SetInt(int64(from))
	reflect.ValueOf(&w).Elem().FieldByName("To").SetInt(int64(to))
	return config.QuietHours{Location: loc, Windows: []config.Window{w}}
}

// gaps lists 2026 spring-forward transitions: the instant the clock jumps, and a wall time inside the gap.
var gaps = []struct {
	zone       string
	transition time.Time
	gapMinute  int // minutes since midnight, strictly inside the gap
}{
	{"America/Chicago", utc(2026, 3, 8, 8, 0), 2*60 + 30},
	{"Europe/Berlin", utc(2026, 3, 29, 1, 0), 2*60 + 30},
	{"Australia/Sydney", utc(2026, 10, 3, 16, 0), 2*60 + 30},
	{"Australia/Lord_Howe", utc(2026, 10, 3, 15, 30), 2*60 + 15}, // the gap is 30 minutes
	{"America/Santiago", utc(2026, 9, 6, 4, 0), 30},              // the gap is at midnight
}

func TestQuietGapEndIsTheTransition(t *testing.T) {
	for _, g := range gaps {
		q := oneWindow(zone(t, g.zone), 22*60, g.gapMinute)
		end, inside := core.QuietEnd(q, g.transition.Add(-10*time.Minute))
		if !inside || !end.Equal(g.transition) {
			t.Errorf("%s: end = %v, inside = %v; want inside until %v", g.zone, end.UTC(), inside, g.transition)
		}
		if _, inside := core.QuietEnd(q, g.transition); inside {
			t.Errorf("%s: the transition instant is inside a window ending in the gap", g.zone)
		}
	}
}

func TestQuietGapStartIsTheTransition(t *testing.T) {
	for _, g := range gaps {
		loc := zone(t, g.zone)
		q := oneWindow(loc, g.gapMinute, 5*60)
		if _, inside := core.QuietEnd(q, g.transition.Add(-time.Minute)); inside {
			t.Errorf("%s: a window starting in the gap began before it", g.zone)
		}
		end, inside := core.QuietEnd(q, g.transition)
		local := g.transition.In(loc)
		want := time.Date(local.Year(), local.Month(), local.Day(), 5, 0, 0, 0, loc)
		if !inside || !end.Equal(want) {
			t.Errorf("%s: end = %v, inside = %v; want inside until %v", g.zone, end, inside, want)
		}
	}
}

func TestQuietWindowWithBothEdgesInGapIsEmpty(t *testing.T) {
	for _, g := range gaps {
		q := oneWindow(zone(t, g.zone), g.gapMinute-10, g.gapMinute+5)
		for _, d := range []time.Duration{-time.Minute, 0, 5 * time.Minute, 30 * time.Minute} {
			if end, inside := core.QuietEnd(q, g.transition.Add(d)); inside {
				t.Errorf("%s: inside at %v for %v until %v", g.zone, d, g.transition, end)
			}
		}
	}
}

func TestQuietFallBackEdgeIsFirstOccurrence(t *testing.T) {
	cases := []struct {
		zone   string
		now    time.Time
		minute int
		first  time.Time
	}{
		{"America/Chicago", utc(2026, 11, 1, 3, 0), 1*60 + 30, utc(2026, 11, 1, 6, 30)},
		{"Europe/Berlin", utc(2026, 10, 24, 20, 0), 2*60 + 30, utc(2026, 10, 25, 0, 30)},
		{"Australia/Lord_Howe", utc(2027, 4, 3, 12, 0), 1*60 + 45, utc(2027, 4, 3, 14, 45)},
	}
	for _, c := range cases {
		q := oneWindow(zone(t, c.zone), 22*60, c.minute)
		end, inside := core.QuietEnd(q, c.now)
		if !inside || !end.Equal(c.first) {
			t.Errorf("%s: end = %v, inside = %v; want the first occurrence %v", c.zone, end.UTC(), inside, c.first)
		}
	}
}

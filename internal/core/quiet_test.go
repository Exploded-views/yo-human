package core_test

import (
	"testing"
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
	"github.com/Exploded-views/yo-human/internal/core"
)

func chicago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	return loc
}

func nightly(loc *time.Location) config.QuietHours {
	return config.QuietHours{Location: loc, Windows: []config.Window{{From: 22 * 60, To: 7 * 60}}}
}

func TestQuietWindowAcrossMidnightAndDST(t *testing.T) {
	loc := chicago(t)
	q := nightly(loc)
	cases := []struct {
		name   string
		now    time.Time
		inside bool
		end    time.Time
	}{
		{"before midnight on the night DST ends", time.Date(2026, 10, 31, 23, 30, 0, 0, loc), true, time.Date(2026, 11, 1, 7, 0, 0, 0, loc)},
		{"after the fall-back hour", time.Date(2026, 11, 1, 6, 30, 0, 0, loc), true, time.Date(2026, 11, 1, 7, 0, 0, 0, loc)},
		{"at the end", time.Date(2026, 11, 1, 7, 0, 0, 0, loc), false, time.Time{}},
		{"one minute before the start", time.Date(2026, 11, 1, 21, 59, 0, 0, loc), false, time.Time{}},
		{"at the start", time.Date(2026, 11, 1, 22, 0, 0, 0, loc), true, time.Date(2026, 11, 2, 7, 0, 0, 0, loc)},
		{"asked in UTC", time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), true, time.Date(2026, 11, 1, 7, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		end, inside := core.QuietEnd(q, c.now)
		if inside != c.inside || !end.Equal(c.end) {
			t.Errorf("%s: QuietEnd = %v, %v; want %v, %v", c.name, end, inside, c.end, c.inside)
		}
	}
}

func TestQuietEndIsInTheConfiguredLocation(t *testing.T) {
	loc := chicago(t)
	end, inside := core.QuietEnd(nightly(loc), time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC))
	if !inside || end.Location() != loc || end.Hour() != 7 {
		t.Errorf("end = %v (%v), inside = %v; want 07:00 in %v", end, end.Location(), inside, loc)
	}
}

func TestQuietDaysFilter(t *testing.T) {
	loc := chicago(t)
	q := config.QuietHours{Location: loc, Windows: []config.Window{
		{Days: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}, From: 9 * 60, To: 17 * 60},
	}}
	sat := time.Date(2026, 10, 31, 12, 0, 0, 0, loc) // a Saturday
	if _, inside := core.QuietEnd(q, sat); inside {
		t.Error("a weekday window applied on Saturday")
	}
	fri := time.Date(2026, 10, 30, 12, 0, 0, 0, loc)
	if end, inside := core.QuietEnd(q, fri); !inside || end.Hour() != 17 {
		t.Errorf("Friday noon: %v, %v; want inside until 17:00", end, inside)
	}
}

func TestQuietDayIsTheDayTheWindowStarts(t *testing.T) {
	loc := chicago(t)
	q := config.QuietHours{Location: loc, Windows: []config.Window{{Days: []time.Weekday{time.Friday}, From: 22 * 60, To: 7 * 60}}}
	satEarly := time.Date(2026, 10, 31, 3, 0, 0, 0, loc) // Friday night's window
	if _, inside := core.QuietEnd(q, satEarly); !inside {
		t.Error("Saturday 03:00 is the tail of Friday's window")
	}
	friEarly := time.Date(2026, 10, 30, 3, 0, 0, 0, loc) // tail of Thursday's window
	if _, inside := core.QuietEnd(q, friEarly); inside {
		t.Error("Friday 03:00 belongs to Thursday's window, which is not configured")
	}
	satLate := time.Date(2026, 10, 31, 23, 0, 0, 0, loc)
	if _, inside := core.QuietEnd(q, satLate); inside {
		t.Error("Saturday 23:00 starts a Saturday window, which is not configured")
	}
}

func TestQuietNoWindowsOrNoLocation(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 10, 31, 23, 30, 0, 0, loc)
	if _, inside := core.QuietEnd(config.QuietHours{Location: loc}, now); inside {
		t.Error("no windows still reported quiet hours")
	}
	if _, inside := core.QuietEnd(config.QuietHours{Windows: nightly(loc).Windows}, now); inside {
		t.Error("a nil location still reported quiet hours")
	}
	if _, inside := core.QuietEnd(config.QuietHours{}, now); inside {
		t.Error("the zero value reported quiet hours")
	}
}

func TestQuietLaterEndingWindowWins(t *testing.T) {
	loc := chicago(t)
	q := config.QuietHours{Location: loc, Windows: []config.Window{
		{From: 22 * 60, To: 6 * 60}, {From: 23 * 60, To: 8 * 60},
	}}
	end, inside := core.QuietEnd(q, time.Date(2026, 10, 31, 23, 30, 0, 0, loc))
	if !inside || end.Hour() != 8 {
		t.Errorf("end = %v, inside = %v; want 08:00", end, inside)
	}
}

func TestQuietSameDayWindow(t *testing.T) {
	loc := chicago(t)
	q := config.QuietHours{Location: loc, Windows: []config.Window{{From: 13 * 60, To: 14 * 60}}}
	if _, inside := core.QuietEnd(q, time.Date(2026, 10, 31, 22, 0, 0, 0, loc)); inside {
		t.Error("a 13:00-14:00 window applied at 22:00")
	}
	if _, inside := core.QuietEnd(q, time.Date(2026, 10, 31, 12, 59, 0, 0, loc)); inside {
		t.Error("a 13:00-14:00 window applied at 12:59")
	}
}

func TestQuietEastOfUTC(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	q := config.QuietHours{Location: loc, Windows: []config.Window{{Days: []time.Weekday{time.Saturday}, From: 1 * 60, To: 5 * 60}}}
	// 03:00 in Tokyo on Saturday is still Friday in UTC.
	now := time.Date(2026, 10, 31, 3, 0, 0, 0, loc)
	end, inside := core.QuietEnd(q, now)
	if !inside || !end.Equal(time.Date(2026, 10, 31, 5, 0, 0, 0, loc)) {
		t.Errorf("QuietEnd = %v, %v; want inside until 05:00 Tokyo", end, inside)
	}
}

func TestQuietSpringForwardGapEnd(t *testing.T) {
	loc := chicago(t)
	q := config.QuietHours{Location: loc, Windows: []config.Window{{From: 22 * 60, To: 2*60 + 30}}}
	want := time.Date(2026, 3, 8, 3, 0, 0, 0, loc) // 03:00 CDT, the first instant after the gap
	for _, now := range []time.Time{time.Date(2026, 3, 8, 1, 40, 0, 0, loc), time.Date(2026, 3, 7, 23, 0, 0, 0, loc)} {
		end, inside := core.QuietEnd(q, now)
		if !inside || !end.Equal(want) {
			t.Errorf("QuietEnd(%v) = %v, %v; want inside until %v", now, end, inside, want)
		}
	}
	if _, inside := core.QuietEnd(q, want); inside {
		t.Error("the transition instant itself is outside the window")
	}
}

func TestQuietSpringForwardGapStart(t *testing.T) {
	loc := chicago(t)
	q := config.QuietHours{Location: loc, Windows: []config.Window{{From: 2*60 + 30, To: 5 * 60}}}
	if _, inside := core.QuietEnd(q, time.Date(2026, 3, 8, 1, 59, 0, 0, loc)); inside {
		t.Error("window with a start in the gap began before the gap")
	}
	end, inside := core.QuietEnd(q, time.Date(2026, 3, 8, 3, 0, 0, 0, loc))
	if !inside || !end.Equal(time.Date(2026, 3, 8, 5, 0, 0, 0, loc)) {
		t.Errorf("at the transition: %v, %v; want inside until 05:00 CDT", end, inside)
	}
}

func TestQuietFallBackEdgeIsTheFirstOccurrence(t *testing.T) {
	loc := chicago(t)
	q := config.QuietHours{Location: loc, Windows: []config.Window{{From: 22 * 60, To: 1*60 + 30}}}
	end, inside := core.QuietEnd(q, time.Date(2026, 10, 31, 23, 0, 0, 0, loc))
	first := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) // 01:30 CDT, UTC-5
	if !inside || !end.Equal(first) {
		t.Errorf("end = %v, %v; want the first 01:30 (%v)", end, inside, first)
	}
}

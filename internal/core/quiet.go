package core

import (
	"time"

	"github.com/Exploded-views/yo-human/internal/config"
)

// QuietEnd reports whether now is inside a quiet-hours window and, if so, when that window ends.
// All arithmetic is done in q.Location. A window belongs to the day it starts on, and one whose To is
// not after its From crosses midnight. A window with no Days applies every day. A nil Location or no
// windows means no quiet hours. A window whose From equals its To is a full 24 hours.
// An edge whose wall time does not exist (a spring-forward gap) falls at the transition instant; one
// that happens twice (fall back) falls at its first occurrence. When windows overlap, the latest end wins.
func QuietEnd(q config.QuietHours, now time.Time) (end time.Time, inside bool) {
	if q.Location == nil {
		return time.Time{}, false
	}
	local := now.In(q.Location)
	for _, w := range q.Windows {
		for back := 0; back <= 1; back++ {
			day := local.AddDate(0, 0, -back)
			s, e, ok := span(w, day, q.Location)
			if ok && !local.Before(s) && local.Before(e) && e.After(end) {
				end, inside = e, true
			}
		}
	}
	return end, inside
}

// span is the window as it starts on day's calendar date, and whether it runs on that weekday.
func span(w config.Window, day time.Time, loc *time.Location) (start, end time.Time, ok bool) {
	if !runsOn(w, day.Weekday()) {
		return start, end, false
	}
	y, m, d := day.Date()
	from, to := int(w.From), int(w.To)
	endDay := d
	if to <= from {
		endDay++
	}
	start = wall(y, m, d, from, loc)
	end = wall(y, m, endDay, to, loc)
	return start, end, true
}

func runsOn(w config.Window, wd time.Weekday) bool {
	if len(w.Days) == 0 {
		return true
	}
	for _, d := range w.Days {
		if d == wd {
			return true
		}
	}
	return false
}

// wall is the instant of minute-of-day mins on a calendar date in loc, whichever side of UTC loc is.
//
// naive is the request read as UTC. The offset in force a day before and a day after it brackets any
// transition near that date, so naive minus each offset gives the only instants that can show the
// requested wall time in loc. Candidates whose wall clock in loc really reads the request are valid:
// two valid means the wall time repeats (fall back) and the earlier one is the first occurrence; none
// means it never happened (a gap). The edge is then the transition itself, the start of the zone
// holding naive minus the offset before the gap, which is already past the gap. A gap implies a transition before it, so that start is never the zero time.
func wall(y int, m time.Month, d, mins int, loc *time.Location) time.Time {
	naive := time.Date(y, m, d, mins/60, mins%60, 0, 0, time.UTC)
	offBefore := offset(naive.Add(-24*time.Hour), loc)
	offAfter := offset(naive.Add(24*time.Hour), loc)
	var first time.Time
	for _, off := range []int{offBefore, offAfter} {
		c := naive.Add(-time.Duration(off) * time.Second)
		if !sameWall(c.In(loc), naive) {
			continue
		}
		if first.IsZero() || c.Before(first) {
			first = c
		}
	}
	if !first.IsZero() {
		return first.In(loc)
	}
	after := naive.Add(-time.Duration(offBefore) * time.Second).In(loc)
	start, _ := after.ZoneBounds()
	return start
}

func offset(t time.Time, loc *time.Location) int {
	_, off := t.In(loc).Zone()
	return off
}

// sameWall reports whether local shows the same calendar date and clock time as naive (read as UTC).
func sameWall(local, naive time.Time) bool {
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, time.UTC).Equal(naive)
}

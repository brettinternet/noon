package schedule

import "time"

// Next returns the first occurrence strictly after after, in loc. Intervals
// simply add their duration; retaining an interval's phase is the caller's job.
// A nil location means UTC. The zero Schedule returns the zero time.
func (s Schedule) Next(after time.Time, loc *time.Location) time.Time {
	if s.duration > 0 {
		return after.Add(s.duration)
	}
	if s.text == "" {
		return time.Time{}
	}
	if loc == nil {
		loc = time.UTC
	}
	local := after.In(loc)
	// Walk wall time in UTC so timezone normalization cannot skip a gap candidate.
	w := wall(local)
	// A wildcard step can intersect a month-day with a weekday. Such matches
	// can be more than eight years apart; the Gregorian calendar repeats in 400.
	limit := w.AddDate(400, 0, 0)
	for !w.After(limit) {
		y, m, d := w.Date()
		h, minute, _ := w.Clock()
		switch {
		case !s.has(3, int(m)):
			w = time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
			continue
		case !s.matchesDay(w):
			w = time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
			continue
		case !s.has(1, h):
			w = time.Date(y, m, d, h+1, 0, 0, 0, time.UTC)
			continue
		case !s.has(0, minute):
			w = w.Add(time.Minute)
			continue
		}
		candidate := resolveWall(w, loc)
		if candidate.After(after) {
			return candidate
		}
		w = w.Add(time.Minute)
	}
	return time.Time{}
}

func (s Schedule) matchesDay(w time.Time) bool {
	dom, dow := s.has(2, w.Day()), s.has(4, int(w.Weekday()))
	if s.dayOR {
		return dom || dow
	}
	return dom && dow
}

func wall(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
}

func resolveWall(w time.Time, loc *time.Location) time.Time {
	u := time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), 0, 0, loc)
	shown := wall(u)
	start, end := u.ZoneBounds()
	if !shown.Equal(w) {
		// Go normalizes a nonexistent local time to either side of a gap.
		if shown.Before(w) {
			return end.In(loc)
		}
		return start.In(loc)
	}
	// Go may choose the second occurrence of an ambiguous wall time. The zone
	// immediately before its start supplies the other offset; accept it only if
	// it reproduces the exact requested wall time.
	if !start.IsZero() {
		_, previous := start.Add(-time.Nanosecond).Zone()
		earlier := w.Add(-time.Duration(previous) * time.Second).In(loc)
		if earlier.Before(u) && wall(earlier).Equal(w) {
			return earlier
		}
	}
	return u
}

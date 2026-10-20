package schedule

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SplayOffset is a stable whole-second offset in [0, splay). Project must be
// the canonical project path. A zero splay leaves scheduling unchanged.
func SplayOffset(project, name string, splay time.Duration) time.Duration {
	seconds := splay / time.Second
	if seconds <= 0 {
		return 0
	}
	sum := sha256.Sum256([]byte(project + "\x00" + name))
	return time.Duration(binary.BigEndian.Uint64(sum[:8])%uint64(seconds)) * time.Second
}

// JitterOffset is a stable whole-second offset in [0, jitter) for one nominal
// occurrence. The nominal Unix second makes each occurrence vary independently.
func JitterOffset(project, name string, nominal time.Time, jitter time.Duration) time.Duration {
	seconds := jitter / time.Second
	if seconds <= 0 {
		return 0
	}
	seed := project + "\x00" + name + "\x00" + strconv.FormatInt(nominal.Unix(), 10)
	sum := sha256.Sum256([]byte(seed))
	return time.Duration(binary.BigEndian.Uint64(sum[:8])%uint64(seconds)) * time.Second
}

// FormatDuration renders nonnegative whole-second durations in the accepted
// s, m, h, d form, with zero-valued trailing units omitted.
func FormatDuration(value time.Duration) string {
	if value < 0 || value%time.Second != 0 {
		return value.String()
	}
	if value == 0 {
		return "0s"
	}
	var out strings.Builder
	remaining := value
	for _, unit := range []struct {
		duration time.Duration
		name     string
	}{{24 * time.Hour, "d"}, {time.Hour, "h"}, {time.Minute, "m"}, {time.Second, "s"}} {
		if count := remaining / unit.duration; count > 0 {
			fmt.Fprintf(&out, "%d%s", count, unit.name)
			remaining %= unit.duration
		}
	}
	return out.String()
}

// EffectiveStart returns the nominal occurrence shifted forward by its stable
// per-job splay and per-occurrence jitter.
func EffectiveStart(project, name string, nominal time.Time, splay, jitter time.Duration) time.Time {
	return nominal.Add(SplayOffset(project, name, splay) + JitterOffset(project, name, nominal, jitter))
}

// NextWithOffset applies a fixed forward offset to a resolved occurrence.
func (s Schedule) NextWithOffset(after time.Time, loc *time.Location, offset time.Duration) time.Time {
	next := s.Next(after.Add(-offset), loc)
	if next.IsZero() {
		return next
	}
	return next.Add(offset)
}

// NextEffective returns the next nominal occurrence and its effective start.
// For intervals, intervalPhase preserves an existing nominal phase; a zero
// phase previews the first interval from after, without persisted state.
func (s Schedule) NextEffective(after time.Time, loc *time.Location, project, name string, splay, jitter time.Duration, intervalPhase time.Time) (nominal, effective time.Time) {
	splayOffset := SplayOffset(project, name, splay)
	searchFrom := after.Add(-splayOffset - jitter)
	if s.duration > 0 {
		if intervalPhase.IsZero() {
			nominal = s.Next(searchFrom, loc)
		} else {
			if searchFrom.Before(intervalPhase) {
				searchFrom = intervalPhase
			}
			delta := searchFrom.Sub(intervalPhase)
			if delta < 0 {
				nominal = intervalPhase
			} else {
				nominal = intervalPhase.Add((delta / s.duration) * s.duration)
				if nominal.Before(searchFrom) {
					nominal = nominal.Add(s.duration)
				}
			}
		}
	} else {
		nominal = s.Next(searchFrom, loc)
	}
	for !nominal.IsZero() {
		effective = EffectiveStart(project, name, nominal, splay, jitter)
		if effective.After(after) {
			return nominal, effective
		}
		if s.duration > 0 {
			nominal = nominal.Add(s.duration)
		} else {
			nominal = s.Next(nominal, loc)
		}
	}
	return time.Time{}, time.Time{}
}

// StateKey preserves pre-splay keys for jobs that omit splay and preserves
// existing splay keys when jitter is unset. Jitter changes reset schedule state.
func StateKey(text, timezone, project, name string, splay, jitter time.Duration) string {
	key := text + "|" + timezone
	if splay > 0 {
		key += fmt.Sprintf("|splay=%s|offset=%s|project=%q|job=%q", splay, SplayOffset(project, name, splay), project, name)
	}
	if jitter > 0 {
		key += "~" + FormatDuration(jitter)
	}
	return key
}

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

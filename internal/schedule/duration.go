package schedule

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ParseDuration parses nonnegative integer components with s, m, h, and d (24h)
// units. Fractions, signs, missing units, and overflow are errors.
func ParseDuration(text string) (time.Duration, error) {
	original := text
	var total time.Duration
	if text == "" {
		return 0, fmt.Errorf("duration is empty: use 90s, 1h30m, or 1d12h")
	}
	for text != "" {
		i := 0
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		if i == 0 || i == len(text) {
			return 0, fmt.Errorf("invalid duration %q: use s, m, h, or d units", original)
		}
		n, err := strconv.ParseUint(text[:i], 10, 64)
		var unit time.Duration
		switch text[i] {
		case 's':
			unit = time.Second
		case 'm':
			unit = time.Minute
		case 'h':
			unit = time.Hour
		case 'd':
			unit = 24 * time.Hour
		default:
			return 0, fmt.Errorf("invalid duration unit: use s, m, h, or d")
		}
		if err != nil || n > uint64((math.MaxInt64-int64(total))/int64(unit)) {
			return 0, fmt.Errorf("duration %q overflows", original)
		}
		total += time.Duration(n) * unit
		text = text[i+1:]
	}
	return total, nil
}

func parseInterval(text string) (Schedule, error) {
	parts := strings.Fields(text)
	if len(parts) == 1 {
		i := 0
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		if i > 0 && i < len(text) && text[i:] != "s" && !strings.ContainsAny(text[i:], "0123456789") {
			parts = []string{text[:i], text[i:]}
		}
	}
	if len(parts) == 2 {
		units := map[string]string{"m": "m", "min": "m", "mins": "m", "minute": "m", "minutes": "m", "h": "h", "hr": "h", "hrs": "h", "hour": "h", "hours": "h", "d": "d", "day": "d", "days": "d"}
		unit, ok := units[parts[1]]
		if !ok {
			return Schedule{}, fmt.Errorf("unknown interval unit: try every 1m, every 1h, or every 1d")
		}
		if strings.IndexFunc(parts[0], func(c rune) bool { return c < '0' || c > '9' }) >= 0 {
			return Schedule{}, fmt.Errorf("interval count must be a nonnegative integer")
		}
		text = parts[0] + unit
	}
	d, err := ParseDuration(text)
	if err != nil {
		return Schedule{}, err
	}
	if d < time.Minute || d > 365*24*time.Hour || d%time.Minute != 0 {
		return Schedule{}, fmt.Errorf("intervals must be whole minutes from 1m to 365d: try every 1m")
	}
	var b strings.Builder
	b.WriteString("every ")
	remaining := d
	for _, u := range []struct {
		d    time.Duration
		name string
	}{{24 * time.Hour, "d"}, {time.Hour, "h"}, {time.Minute, "m"}} {
		if n := remaining / u.d; n > 0 {
			fmt.Fprintf(&b, "%d%s", n, u.name)
			remaining %= u.d
		}
	}
	return Schedule{text: b.String(), duration: d}, nil
}

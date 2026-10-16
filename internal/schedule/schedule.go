// Package schedule parses schedules and computes occurrences without reading the clock.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is an immutable parsed schedule. Its zero value has no occurrences.
type Schedule struct {
	text     string
	duration time.Duration
	fields   [5]uint64
	dayOR    bool
}

// String returns the normalized display form.
func (s Schedule) String() string { return s.text }

// Interval returns the elapsed duration, only for interval schedules.
func (s Schedule) Interval() (time.Duration, bool) { return s.duration, s.duration > 0 }

// Parse accepts five-field cron, macros, calendar phrases, or elapsed intervals.
func Parse(text string) (Schedule, error) {
	text = strings.ToLower(strings.Join(strings.Fields(text), " "))
	switch text {
	case "every day":
		return Schedule{}, fmt.Errorf("every day needs a time: try every day at 9am, daily (midnight), or every 24h")
	case "every hour":
		return Schedule{}, fmt.Errorf("every hour is ambiguous: try hourly or every 1h")
	case "every minute":
		return Schedule{}, fmt.Errorf("every minute is ambiguous: try * * * * * or every 1m")
	case "@reboot":
		//lint:ignore ST1005 Noon is a proper noun in the specified user-facing diagnostic.
		return Schedule{}, fmt.Errorf("Noon has no boot trigger")
	}
	if strings.HasPrefix(text, "@every ") {
		return Schedule{}, fmt.Errorf("use %s instead", strings.TrimPrefix(text, "@"))
	}
	macros := map[string]string{"hourly": "0 * * * *", "daily": "0 0 * * *", "midnight": "0 0 * * *", "weekly": "0 0 * * 0", "monthly": "0 0 1 * *", "yearly": "0 0 1 1 *", "annually": "0 0 1 1 *"}
	macro := strings.TrimPrefix(text, "@")
	if cron, ok := macros[macro]; ok && (strings.HasPrefix(text, "@") || (macro != "midnight" && macro != "annually")) {
		s, err := parseCron(cron)
		s.text = "@" + macro
		return s, err
	}
	if strings.Contains(text, " at ") || strings.HasPrefix(text, "at ") {
		return parseCalendar(text)
	}
	if strings.HasPrefix(text, "every ") {
		return parseInterval(strings.TrimPrefix(text, "every "))
	}
	return parseCron(text)
}

var monthNames = strings.Fields("jan feb mar apr may jun jul aug sep oct nov dec")
var dayNames = strings.Fields("sunday monday tuesday wednesday thursday friday saturday")

func parseCron(text string) (Schedule, error) {
	parts := strings.Fields(text)
	if len(parts) == 6 {
		//lint:ignore ST1005 Noon is a proper noun in the specified user-facing diagnostic.
		return Schedule{}, fmt.Errorf("Noon uses five-field cron, without seconds")
	}
	if len(parts) != 5 {
		return Schedule{}, fmt.Errorf("expected five-field cron or a schedule such as weekdays at 9am")
	}
	s := Schedule{text: strings.Join(parts, " "), dayOR: !strings.HasPrefix(parts[2], "*") && !strings.HasPrefix(parts[4], "*")}
	mins := [5]int{0, 0, 1, 1, 0}
	maxs := [5]int{59, 23, 31, 12, 7}
	for i, part := range parts {
		bits, err := parseField(part, mins[i], maxs[i], i)
		if err != nil {
			return Schedule{}, fmt.Errorf("cron field %d: %w", i+1, err)
		}
		s.fields[i] = bits
	}
	if !s.dayOR {
		possible := false
		for m, days := range []int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31} {
			if !s.has(3, m+1) {
				continue
			}
			for d := 1; d <= days; d++ {
				if s.has(2, d) {
					possible = true
				}
			}
		}
		if !possible {
			return Schedule{}, fmt.Errorf("cron expression can never match: choose a day that exists in an allowed month")
		}
	}
	return s, nil
}

func parseField(text string, min, max, field int) (uint64, error) {
	var bits uint64
	for _, item := range strings.Split(text, ",") {
		base, stepText, stepped := strings.Cut(item, "/")
		step := 1
		if stepped {
			var err error
			step, err = number(stepText)
			if err != nil || step == 0 {
				return 0, fmt.Errorf("invalid step %q: use a positive integer", stepText)
			}
		}
		lo, hi := min, max
		if base != "*" {
			first, last, ranged := strings.Cut(base, "-")
			var err error
			lo, err = fieldNumber(first, field)
			if err != nil {
				return 0, err
			}
			hi = lo
			if ranged {
				hi, err = fieldNumber(last, field)
			} else if stepped {
				hi = max
			}
			if err != nil {
				return 0, err
			}
		}
		if lo < min || hi > max || lo > max || hi < min {
			return 0, fmt.Errorf("value out of range %d-%d", min, max)
		}
		if lo > hi {
			return 0, fmt.Errorf("wrap-around ranges are not supported")
		}
		for n := lo; n <= hi; {
			v := n
			if field == 4 && v == 7 {
				v = 0
			}
			bits |= uint64(1) << v
			if step > hi-n {
				break
			} // Avoid overflow for large valid steps.
			n += step
		}
	}
	return bits, nil
}

func fieldNumber(text string, field int) (int, error) {
	if field == 3 {
		for i, name := range monthNames {
			if text == name {
				return i + 1, nil
			}
		}
	}
	if field == 4 {
		for i, name := range dayNames {
			if text == name[:3] {
				return i, nil
			}
		}
	}
	return number(text)
}

func number(text string) (int, error) {
	if text == "" {
		return 0, fmt.Errorf("expected a nonnegative integer")
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid integer %q", text)
		}
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("integer out of range: %q", text)
	}
	return n, nil
}

func (s Schedule) has(field, value int) bool { return s.fields[field]&(uint64(1)<<value) != 0 }

func parseCalendar(text string) (Schedule, error) {
	days, clock, ok := strings.Cut(text, " at ")
	if !ok {
		return Schedule{}, fmt.Errorf("specify a day and time, such as every day at 9am or every day at 9:00")
	}
	hour, minute, err := parseClock(clock)
	if err != nil {
		return Schedule{}, err
	}
	dow := "*"
	display := days
	switch days {
	case "every day", "daily":
		display = "every day"
	case "weekdays":
		dow = "1-5"
	case "weekends":
		dow = "0,6"
	default:
		list := strings.TrimPrefix(days, "every ")
		var selected [7]bool
		for _, day := range strings.Split(list, ",") {
			found := false
			for i, name := range dayNames {
				if day == name || day == name[:3] {
					selected[i] = true
					found = true
					break
				}
			}
			if !found {
				return Schedule{}, fmt.Errorf("unknown day %q: try weekdays at 9am", day)
			}
		}
		var names, nums []string
		for _, i := range []int{1, 2, 3, 4, 5, 6, 0} {
			if selected[i] {
				names = append(names, dayNames[i][:3])
				nums = append(nums, strconv.Itoa(i))
			}
		}
		display = strings.Join(names, ",")
		if len(nums) == 1 {
			n, _ := strconv.Atoi(nums[0])
			display = "every " + dayNames[n]
		}
		dow = strings.Join(nums, ",")
	}
	s, err := parseCron(fmt.Sprintf("%d %d * * %s", minute, hour, dow))
	s.text = fmt.Sprintf("%s at %02d:%02d", display, hour, minute)
	return s, err
}

func parseClock(text string) (int, int, error) {
	if text == "noon" {
		return 12, 0, nil
	}
	if text == "midnight" {
		return 0, 0, nil
	}
	invalid := fmt.Errorf("invalid time %q: use 9am or 9:00 (one time only)", text)
	meridian := ""
	if strings.HasSuffix(text, "am") || strings.HasSuffix(text, "pm") {
		meridian = text[len(text)-2:]
		text = text[:len(text)-2]
	}
	h, m, colon := strings.Cut(text, ":")
	if len(h) < 1 || len(h) > 2 || (!colon && meridian == "") {
		return 0, 0, invalid
	}
	hour, err := number(h)
	if err != nil {
		return 0, 0, invalid
	}
	minute := 0
	if colon {
		minute, err = number(m)
		if err != nil || len(m) != 2 || minute > 59 {
			return 0, 0, invalid
		}
	}
	if meridian != "" {
		if hour < 1 || hour > 12 {
			return 0, 0, invalid
		}
		hour %= 12
		if meridian == "pm" {
			hour += 12
		}
	} else if hour > 23 {
		return 0, 0, invalid
	}
	return hour, minute, nil
}

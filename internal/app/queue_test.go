package app

import (
	"testing"
	"time"
)

// Queued due times and job ends are compared, so --at must resolve local wall
// times with the same DST rules as until and schedules.
func TestQueueDueAtMatchesUntilAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, location)
	for _, test := range []struct{ value, want string }{
		{"2026-03-08 02:30", "2026-03-08T03:00:00-06:00"}, // skipped: first instant after the gap
		{"2026-11-01 01:30", "2026-11-01T01:30:00-06:00"}, // repeated: first occurrence
	} {
		due, err := QueueDueAt("at", "", test.value, now, location)
		if err != nil {
			t.Fatalf("--at %q: %v", test.value, err)
		}
		until, err := ResolveEnd("", test.value, now, location)
		if err != nil {
			t.Fatalf("--until %q: %v", test.value, err)
		}
		if got := due.Format(time.RFC3339); got != test.want || !due.Equal(until) {
			t.Fatalf("%q: --at %s, --until %s, want %s", test.value, got, until.Format(time.RFC3339), test.want)
		}
	}
}

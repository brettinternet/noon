package schedule_test

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/brettinternet/noon/internal/schedule"
)

func TestSplay(t *testing.T) {
	// A fixed vector catches changes to seeding, byte order, and modulo.
	if got := schedule.SplayOffset("/repo", "triage", 10*time.Minute); got != 487*time.Second {
		t.Fatalf("offset = %s, want 8m7s", got)
	}
	for _, project := range []string{"/repo", "/other/repo", "/repo/with spaces"} {
		for _, name := range []string{"triage", "tests", "review"} {
			for _, window := range []time.Duration{time.Second, 17 * time.Second, 10 * time.Minute, time.Hour} {
				got := schedule.SplayOffset(project, name, window)
				if got < 0 || got >= window || got%time.Second != 0 || got != schedule.SplayOffset(project, name, window) {
					t.Fatalf("%s %s %s: invalid or unstable offset %s", project, name, window, got)
				}
			}
		}
	}
	if schedule.SplayOffset("/repo", "triage", 0) != 0 {
		t.Fatal("unset splay shifted time")
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ text, after, want string }{
		{"every day at 9am", "2026-10-01T09:02:00-04:00", "2026-10-01T09:05:00-04:00"},
		{"every day at 9am", "2026-10-01T09:05:00-04:00", "2026-10-02T09:05:00-04:00"},
		{"every day at 2:30am", "2026-03-08T01:59:00-05:00", "2026-03-08T03:05:00-04:00"},
		{"every day at 1:30am", "2026-11-01T01:34:00-04:00", "2026-11-01T01:35:00-04:00"},
	} {
		s, err := schedule.Parse(tc.text)
		if err != nil {
			t.Fatal(err)
		}
		after, _ := time.Parse(time.RFC3339, tc.after)
		if got := s.NextWithOffset(after, loc, 5*time.Minute).Format(time.RFC3339); got != tc.want {
			t.Errorf("%s after %s: got %s, want %s", tc.text, tc.after, got, tc.want)
		}
	}
}

func TestScheduleParse(t *testing.T) {
	for _, tt := range []struct{ input, want, err string }{
		{"  0  9 * JAN-MAR MON-FRI  ", "0 9 * jan-mar mon-fri", ""},
		{"1,5-10/2,30/15 */3 * * 0,7", "1,5-10/2,30/15 */3 * * 0,7", ""},
		{"0 0 30 2 mon", "0 0 30 2 mon", ""},
		{"@hourly", "@hourly", ""}, {"hourly", "@hourly", ""},
		{"daily", "@daily", ""}, {"@daily", "@daily", ""}, {"@midnight", "@midnight", ""},
		{"weekly", "@weekly", ""}, {"@weekly", "@weekly", ""},
		{"monthly", "@monthly", ""}, {"@monthly", "@monthly", ""},
		{"yearly", "@yearly", ""}, {"@yearly", "@yearly", ""}, {"@annually", "@annually", ""},
		{"every day at 9am", "every day at 09:00", ""},
		{"daily at 12am", "every day at 00:00", ""},
		{"daily at 12pm", "every day at 12:00", ""},
		{"weekdays at 9:30pm", "weekdays at 21:30", ""},
		{"weekends at noon", "weekends at 12:00", ""},
		{"every friday at 5pm", "every friday at 17:00", ""},
		{"every FRI at 17:00", "every friday at 17:00", ""},
		{"fri,MON,wednesday at 18:00", "mon,wed,fri at 18:00", ""},
		{"sun,mon at midnight", "mon,sun at 00:00", ""},
		{"saturday at 9:00", "every saturday at 09:00", ""},
		{"every 15m", "every 15m", ""}, {"every 90 minutes", "every 1h30m", ""},
		{"every 1d12h", "every 1d12h", ""}, {"every 2 hours", "every 2h", ""},
		{"every 3 days", "every 3d", ""}, {"every 365d", "every 365d", ""},
		{"every 2hrs", "every 2h", ""}, {"every 120min", "every 2h", ""},
		{"every 1 min", "every 1m", ""}, {"every 2 mins", "every 2m", ""},
		{"every 1 minute", "every 1m", ""}, {"every 1 hr", "every 1h", ""},
		{"every 1 hour", "every 1h", ""}, {"every 1 day", "every 1d", ""},
		{"every 24h", "every 1d", ""},
		{"0 0 30 2 *", "", "never match"}, {"0 0 31 4,6 *", "", "never match"},
		{"0 0 30 2 */2", "", "never match"},
		{"0 0 * * fri-mon", "", "wrap-around"}, {"*/0 * * * *", "", "positive integer"},
		{"0 0 0 * *", "", "out of range"}, {"60 * * * *", "", "out of range"},
		{"0 24 * * *", "", "out of range"}, {"0 0 * 13 *", "", "out of range"},
		{"0 0 * * 8", "", "out of range"}, {"0 0 * january *", "", "invalid integer"},
		{"0 0 * * monday", "", "invalid integer"}, {"0 0 * * 1,", "", "integer"},
		{"*/2/3 * * * *", "", "positive integer"}, {"0-5-7 * * * *", "", "integer"},
		{"+1 * * * *", "", "integer"}, {"* * * * * *", "", "Noon uses five-field cron, without seconds"},
		{"@reboot", "", "Noon has no boot trigger"}, {"@every 5m", "", "every 5m"},
		{"every day", "", "every day at 9am"}, {"every hour", "", "hourly"},
		{"every minute", "", "every 1m"}, {"every 30s", "", "every 1m"},
		{"every 0m", "", "whole minutes"}, {"every 366d", "", "365d"},
		{"every 999999999999999999999d", "", "overflows"},
		{"at 9", "", "9am or every day at 9:00"}, {"daily at 9", "", "9am or 9:00"},
		{"daily at 24:00", "", "9am or 9:00"}, {"daily at 0am", "", "9am or 9:00"},
		{"daily at 13pm", "", "9am or 9:00"}, {"daily at 9:60", "", "9am or 9:00"},
		{"daily at 9:1", "", "9am or 9:00"}, {"daily at 9am,5pm", "", "one time only"},
		{"someday at noon", "", "unknown day"}, {"", "", "five-field"},
	} {
		t.Run(tt.input, func(t *testing.T) {
			s, err := schedule.Parse(tt.input)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Parse = %v; want error containing %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.String() != tt.want {
				t.Fatalf("String = %q; want %q", s.String(), tt.want)
			}
			round, err := schedule.Parse(s.String())
			if err != nil || round.String() != s.String() {
				t.Fatalf("round trip = %q, %v", round.String(), err)
			}
			duration, interval := s.Interval()
			wantInterval := strings.HasPrefix(tt.want, "every ") && !strings.Contains(tt.want, " at ")
			if interval != wantInterval || (interval && duration <= 0) {
				t.Fatalf("unexpected Interval = %v, %v", duration, interval)
			}
		})
	}
}

func TestScheduleNext(t *testing.T) {
	for _, tt := range []struct{ name, input, zone, after, want string }{
		{"lists ranges steps", "1,10-20/5 9-17/2 * * *", "UTC", "2026-01-01T09:15:00Z", "2026-01-01T09:20:00Z"},
		{"value step", "5/20 * * * *", "UTC", "2026-01-01T00:25:00Z", "2026-01-01T00:45:00Z"},
		{"names", "0 9 * FEB MON-FRI", "UTC", "2026-01-31T00:00:00Z", "2026-02-02T09:00:00Z"},
		{"Sunday seven", "0 0 * * 7", "UTC", "2026-01-03T00:00:00Z", "2026-01-04T00:00:00Z"},
		{"Sunday zero", "0 0 * * 0", "UTC", "2026-01-03T00:00:00Z", "2026-01-04T00:00:00Z"},
		{"Vixie weekday", "0 0 30 2 mon", "UTC", "2026-02-01T00:00:00Z", "2026-02-02T00:00:00Z"},
		{"Vixie monthday", "0 0 10 * mon", "UTC", "2026-01-09T00:00:00Z", "2026-01-10T00:00:00Z"},
		{"star step is unrestricted", "0 0 */2 * mon", "UTC", "2026-01-01T00:00:00Z", "2026-01-05T00:00:00Z"},
		{"star list is unrestricted", "0 0 *,1 * mon", "UTC", "2026-01-01T00:00:00Z", "2026-01-05T00:00:00Z"},
		{"leap day", "0 0 29 2 *", "UTC", "2026-01-01T00:00:00Z", "2028-02-29T00:00:00Z"},
		{"century leap gap", "0 0 29 2 *", "UTC", "2096-02-29T00:00:00Z", "2104-02-29T00:00:00Z"},
		{"hourly", "hourly", "UTC", "2026-01-01T00:00:00Z", "2026-01-01T01:00:00Z"},
		{"weekly", "weekly", "UTC", "2026-01-01T00:00:00Z", "2026-01-04T00:00:00Z"},
		{"monthly", "monthly", "UTC", "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"},
		{"yearly", "yearly", "UTC", "2026-01-01T00:00:00Z", "2027-01-01T00:00:00Z"},
		{"daily", "daily", "UTC", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"},
		{"calendar weekdays", "weekdays at 9am", "UTC", "2026-01-02T09:00:00Z", "2026-01-05T09:00:00Z"},
		{"calendar weekends", "weekends at noon", "UTC", "2026-01-02T09:00:00Z", "2026-01-03T12:00:00Z"},
		{"calendar days", "fri,mon at 6pm", "UTC", "2026-01-02T18:00:00Z", "2026-01-05T18:00:00Z"},
		{"subminute strict", "* * * * *", "UTC", "2026-01-01T00:00:00.001Z", "2026-01-01T00:01:00Z"},
		{"interval", "every 90 minutes", "UTC", "2026-01-01T00:00:12.001Z", "2026-01-01T01:30:12.001Z"},
		{"Denver spring", "30 2 * * *", "America/Denver", "2026-03-08T01:59:59-07:00", "2026-03-08T03:00:00-06:00"},
		{"Denver gap coalesces", "0,30 2 * * *", "America/Denver", "2026-03-08T03:00:00-06:00", "2026-03-09T02:00:00-06:00"},
		{"Denver fall first", "30 1 * * *", "America/Denver", "2026-11-01T00:00:00-06:00", "2026-11-01T01:30:00-06:00"},
		{"Denver fall no repeat", "30 1 * * *", "America/Denver", "2026-11-01T01:30:00-06:00", "2026-11-02T01:30:00-07:00"},
		{"Denver inside repeated hour", "30 1 * * *", "America/Denver", "2026-11-01T01:00:00-07:00", "2026-11-02T01:30:00-07:00"},
		{"Denver elapsed DST", "every 1d", "America/Denver", "2026-03-07T12:00:00-07:00", "2026-03-08T13:00:00-06:00"},
		{"Lord Howe spring", "15 2 * * *", "Australia/Lord_Howe", "2026-10-04T01:59:59+10:30", "2026-10-04T02:30:00+11:00"},
		{"Lord Howe fall first", "45 1 * * *", "Australia/Lord_Howe", "2026-04-05T00:00:00+11:00", "2026-04-05T01:45:00+11:00"},
		{"Lord Howe fall no repeat", "45 1 * * *", "Australia/Lord_Howe", "2026-04-05T01:45:00+11:00", "2026-04-06T01:45:00+10:30"},
		{"Lord Howe inside repeated hour", "45 1 * * *", "Australia/Lord_Howe", "2026-04-05T01:30:00+10:30", "2026-04-06T01:45:00+10:30"},
		{"Santiago midnight gap", "daily at midnight", "America/Santiago", "2026-09-05T23:59:59-04:00", "2026-09-06T01:00:00-03:00"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, err := schedule.Parse(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			loc, err := time.LoadLocation(tt.zone)
			if err != nil {
				t.Fatal(err)
			}
			after, err := time.Parse(time.RFC3339Nano, tt.after)
			if err != nil {
				t.Fatal(err)
			}
			want, err := time.Parse(time.RFC3339Nano, tt.want)
			if err != nil {
				t.Fatal(err)
			}
			got := s.Next(after, loc)
			if !got.Equal(want) {
				t.Fatalf("Next = %s; want %s", got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	for _, tt := range []struct {
		input   string
		want    time.Duration
		invalid bool
	}{
		{"90s", 90 * time.Second, false}, {"1h30m", 90 * time.Minute, false}, {"1d12h", 36 * time.Hour, false},
		{"0s", 0, false}, {"9223372036s", 9223372036 * time.Second, false},
		{"9223372037s", 0, true}, {"9223372036s1s", 0, true}, {"18446744073709551616s", 0, true},
		{"1.5h", 0, true}, {"-1s", 0, true}, {"1ms", 0, true}, {"1", 0, true}, {"", 0, true},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := schedule.ParseDuration(tt.input)
			if (err != nil) != tt.invalid || (!tt.invalid && got != tt.want) {
				t.Fatalf("ParseDuration = %v, %v; want %v (invalid %v)", got, err, tt.want, tt.invalid)
			}
		})
	}
}

func FuzzScheduleParse(f *testing.F) {
	for _, seed := range []string{"*/15 * * * *", "0 0 29 2 *", "@daily", "weekdays at 9am", "every 1d12h", "every 30s", "0 0 30 2 mon", "0 0 */2 * mon", "fri,mon at noon"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		s, err := schedule.Parse(text)
		if err != nil {
			return
		}
		round, err := schedule.Parse(s.String())
		if err != nil || round.String() != s.String() {
			t.Fatalf("round trip %q = %q, %v", s.String(), round.String(), err)
		}
		after := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if next := s.Next(after, time.UTC); !next.After(after) {
			t.Fatalf("Next(%q) = %v, not after %v", text, next, after)
		}
	})
}

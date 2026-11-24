package app

import (
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/schedule"
)

func upcomingFor(t *testing.T, id int64, name string, schedules map[string]string, now time.Time) []UpcomingRun {
	t.Helper()
	cfg := config.Config{Project: "/projects/" + name, Timezone: "UTC", Jobs: map[string]config.Job{}}
	for job, text := range schedules {
		parsed, err := schedule.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Jobs[job] = config.Job{Schedule: &parsed, Enabled: true}
	}
	view, err := inspectConfig(cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	return upcomingDay(OverviewProject{ID: id, Name: name, Path: cfg.Project, Timezone: "UTC"}, view.Jobs, now)
}

// A frequent job must not crowd every other start out of the capped list.
func TestUpcomingDayCollapsesFrequentJobsAndCaps(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 30, 0, time.UTC)
	rows := upcomingFor(t, 1, "alpha", map[string]string{"tick": "every 1m", "hourly": "every 1h"}, now)
	var ticks, hourly int
	var last time.Time
	for _, row := range rows {
		switch row.Job {
		case "tick":
			ticks++
			if row.Every != "every 1m" || row.At.After(now.Add(time.Minute)) {
				t.Fatalf("collapsed row = %+v", row)
			}
		case "hourly":
			hourly++
			if row.Every != "" || !last.IsZero() && row.At.Sub(last) != time.Hour {
				t.Fatalf("hourly row collapsed or skipped: %+v after %s", row, last)
			}
			last = row.At
		}
	}
	if ticks != 1 || hourly < 23 {
		t.Fatalf("rows: %d tick, %d hourly; want 1 and one per hour", ticks, hourly)
	}

	// Three projects of hourly jobs exceed the cap; the first 50 starts survive in time order.
	for i, name := range []string{"beta", "gamma"} {
		rows = append(rows, upcomingFor(t, int64(i+2), name, map[string]string{"hourly": "every 1h"}, now)...)
	}
	sorted := sortUpcoming(rows)
	if len(sorted) != upcomingLimit {
		t.Fatalf("len = %d, want %d", len(sorted), upcomingLimit)
	}
	projects := map[string]bool{}
	for i, row := range sorted {
		projects[row.Project] = true
		if i > 0 && row.At.Before(sorted[i-1].At) {
			t.Fatalf("row %d at %s precedes row %d at %s", i, row.At, i-1, sorted[i-1].At)
		}
	}
	if len(projects) != 3 {
		t.Fatalf("capped rows span %v, want all three projects", projects)
	}
}

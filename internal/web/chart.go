package web

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/brettinternet/noon/internal/app"
)

const (
	chartRuns = 50
	// chartSlots keeps bars narrow when a job has few runs.
	chartSlots = 20
	// chartTop is the share of the chart height left above the tallest bar
	// for the timeout label.
	chartTop = 14.0
)

// durationChart is a server-rendered SVG of one job's recent durations.
// Positions are percentages of the chart box, so it scales without a viewBox
// and its text keeps its size.
type durationChart struct {
	Title, Desc string
	Bars        []chartBar
	Legend      []status
	Timeout     *chartLine
}

type chartBar struct {
	ID              int64
	Status          status
	Duration        string
	SlotX, SlotW    float64
	X, W, Y, Height float64
}

type chartLine struct {
	Y     float64
	Label string
}

// newDurationChart takes newest-first finished runs and the job's timeout.
// It returns nil when there are too few runs to compare.
func newDurationChart(job string, runs []app.Run, timeout string) *durationChart {
	runs = slices.DeleteFunc(slices.Clone(runs), func(run app.Run) bool { return run.DurationMS == nil })
	if len(runs) < 2 {
		return nil
	}
	slices.Reverse(runs)

	durations := make([]int64, len(runs))
	slowest := runs[0]
	for i, run := range runs {
		durations[i] = *run.DurationMS
		if *run.DurationMS > *slowest.DurationMS {
			slowest = run
		}
	}
	scale := max(*slowest.DurationMS, 1)

	chart := &durationChart{Title: job + " run durations"}
	timeoutText := ""
	// A timeout far above every run would flatten the bars, so it is drawn
	// only when it is within twice the slowest run.
	if limit, err := time.ParseDuration(timeout); err == nil && limit > 0 && limit.Milliseconds() <= 2*scale {
		ms := limit.Milliseconds()
		scale = max(scale, ms)
		timeoutText = app.DurationText(&ms)
		chart.Timeout = &chartLine{Y: barTop(ms, scale), Label: "timeout " + timeoutText}
	}

	slot := 100 / float64(max(len(runs), chartSlots))
	for i, run := range runs {
		info := statusInfo(run.Status)
		if !slices.Contains(chart.Legend, info) {
			chart.Legend = append(chart.Legend, info)
		}
		y := barTop(*run.DurationMS, scale)
		chart.Bars = append(chart.Bars, chartBar{
			ID: run.ID, Status: info, Duration: app.DurationText(run.DurationMS),
			SlotX: float64(i) * slot, SlotW: slot,
			X: (float64(i) + 0.2) * slot, W: 0.6 * slot,
			Y: y, Height: 100 - y,
		})
	}

	slices.Sort(durations)
	middle := len(durations) / 2
	median := durations[middle]
	if len(durations)%2 == 0 {
		median = (durations[middle-1] + durations[middle]) / 2
	}
	chart.Desc = fmt.Sprintf("Last %d finished runs, oldest first. Median %s, slowest %s (#%s).",
		len(runs), app.DurationText(&median), app.DurationText(slowest.DurationMS), strconv.FormatInt(slowest.ID, 10))
	if timeoutText != "" {
		chart.Desc += " Timeout " + timeoutText + "."
	}
	return chart
}

// barTop maps a duration to the y percentage of its bar's top edge, keeping
// every bar at least 1% tall so short runs stay visible.
func barTop(ms, scale int64) float64 {
	return min(100-(100-chartTop)*float64(ms)/float64(scale), 99)
}

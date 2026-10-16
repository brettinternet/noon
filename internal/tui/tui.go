// Package tui provides the optional terminal UI adapter for Noon.
package tui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
)

const (
	refreshInterval   = time.Second
	logPageBytes      = 64 << 10
	maxFollowLogBytes = 1 << 20
	maxLogBufferBytes = 2*maxFollowLogBytes + 128<<10
)

type screen int

const (
	jobsScreen screen = iota
	runsScreen
	logScreen
	helpScreen
)

type refreshMsg struct{}

var statusFilters = [][]string{
	{},
	{"failed", "timed_out", "interrupted"},
	{"running", "queued"},
}

var statusFilterNames = []string{
	"all",
	"failed,timed_out,interrupted",
	"running,queued",
}

// Model contains the TUI's view state. All state reads and mutations go
// through internal/app, the same operations used by the CLI and MCP clients.
type Model struct {
	input    config.Input
	stateDir string
	project  string

	dashboard app.DashboardView
	screen    screen
	helpFrom  screen
	width     int
	height    int

	selectedJob     int
	selectedJobName string
	runsJob         string
	runs            []app.Run
	selectedRun     int
	selectedRunID   int64
	statusFilter    int

	message         string
	confirmCancelID int64
	logReturn       screen
	logRun          app.Run
	logSection      string
	logSearch       string
	searchPrompt    bool
	searchInput     string
	logText         string
	logCursor       int64
	logSize         int64
	logScroll       int
	logFollowed     bool
	followNotice    bool
	logNotice       string
	autoScroll      bool
}

// NewModel loads the current project dashboard without creating application
// state. Mutations are performed only by explicit keyboard actions.
func NewModel(input config.Input) (*Model, error) {
	cfg, err := config.Load(input)
	if err != nil {
		return nil, app.ConfigError(err)
	}
	model := &Model{
		input: input, stateDir: cfg.StateDir, project: cfg.Project,
		screen: jobsScreen, width: 80, height: 24,
	}
	if err := model.refreshDashboard(); err != nil {
		return nil, err
	}
	return model, nil
}

func (m *Model) Init() tea.Cmd { return refreshTick() }

func refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshMsg{} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch message := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		if m.width < 1 {
			m.width = 80
		}
		if m.height < 1 {
			m.height = 24
		}
		m.logScroll = clamp(m.logScroll, 0, m.maxLogScroll())
		return m, nil
	case refreshMsg:
		m.refresh()
		return m, refreshTick()
	case tea.KeyPressMsg:
		return m.updateKey(message)
	default:
		return m, nil
	}
}

func (m *Model) updateKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := message.Key()
	text := key.Text
	if text == "" && key.Code >= 0x20 && key.Code <= unicode.MaxRune {
		text = string(key.Code)
	}
	if text == "G" || key.ShiftedCode == 'G' {
		text = "G"
	}
	keystroke := message.String()

	if m.searchPrompt {
		switch {
		case key.Code == tea.KeyEscape || key.Code == tea.KeyEsc:
			m.searchPrompt = false
			m.searchInput = ""
			if m.logSearch != "" {
				m.logSearch = ""
				m.message = ""
				m.loadLogTail(true)
			} else {
				m.message = "Search cancelled."
			}
		case key.Code == tea.KeyEnter || key.Code == tea.KeyReturn:
			m.runSearch()
		case key.Code == tea.KeyBackspace || key.Code == tea.KeyDelete:
			m.searchInput = dropLastRune(m.searchInput)
		case keystroke == "ctrl+c":
			return m, tea.Quit
		case text != "":
			m.searchInput += text
		}
		return m, nil
	}

	if m.confirmCancelID != 0 {
		switch strings.ToLower(text) {
		case "y":
			m.cancelRun(m.confirmCancelID)
		case "n":
			m.confirmCancelID = 0
			m.message = "Cancellation cancelled."
		case "esc":
			m.confirmCancelID = 0
			m.message = "Cancellation cancelled."
		default:
			if key.Code == tea.KeyEscape || key.Code == tea.KeyEsc {
				m.confirmCancelID = 0
				m.message = "Cancellation cancelled."
			}
		}
		return m, nil
	}

	if keystroke == "ctrl+c" {
		return m, tea.Quit
	}
	if m.screen == helpScreen {
		if text == "q" {
			return m, tea.Quit
		}
		switch text {
		case "?", "esc":
			m.screen = m.helpFrom
		}
		if key.Code == tea.KeyEscape || key.Code == tea.KeyEsc {
			m.screen = m.helpFrom
		}
		return m, nil
	}
	if text == "q" {
		return m, tea.Quit
	}
	if text == "?" {
		m.helpFrom, m.screen = m.screen, helpScreen
		return m, nil
	}

	if key.Code == tea.KeyEscape || key.Code == tea.KeyEsc || text == "esc" {
		m.goBack()
		return m, nil
	}

	switch m.screen {
	case jobsScreen:
		switch {
		case text == "j" || key.Code == tea.KeyDown:
			m.moveJob(1)
		case text == "k" || key.Code == tea.KeyUp:
			m.moveJob(-1)
		case key.Code == tea.KeyEnter || key.Code == tea.KeyReturn:
			m.openRuns()
		case text == "l":
			m.openLastLog()
		case text == "r":
			m.queueSelectedJob()
		case text == "p":
			m.togglePause()
		}
	case runsScreen:
		switch {
		case text == "j" || key.Code == tea.KeyDown:
			m.moveRun(1)
		case text == "k" || key.Code == tea.KeyUp:
			m.moveRun(-1)
		case text == "f":
			m.statusFilter = (m.statusFilter + 1) % len(statusFilters)
			m.selectedRunID = 0
			m.message = ""
			m.refreshRuns()
		case key.Code == tea.KeyEnter || key.Code == tea.KeyReturn || text == "l":
			m.openSelectedLog()
		case text == "r":
			m.queueSelectedJob()
		case text == "p":
			m.togglePause()
		case text == "c":
			m.askCancel()
		}
	case logScreen:
		switch {
		case text == "j" || key.Code == tea.KeyDown:
			m.scrollLog(1)
		case text == "k" || key.Code == tea.KeyUp:
			m.scrollLog(-1)
		case text == "g":
			m.loadLogTop()
		case text == "G":
			m.logSearch = ""
			m.message = ""
			m.loadLogTail(false)
		case text == "/":
			m.searchPrompt = true
			m.searchInput = ""
			m.message = ""
		case text == "s":
			m.cycleLogSection()
		case text == "c":
			m.askCancel()
		case text == "r":
			m.queueSelectedJob()
		case text == "p":
			m.togglePause()
		}
	}
	return m, nil
}

func (m *Model) View() tea.View {
	lines := make([]string, 0, m.height)
	switch m.screen {
	case jobsScreen:
		lines = m.jobView()
	case runsScreen:
		lines = m.runsView()
	case logScreen:
		lines = m.logView()
	case helpScreen:
		lines = m.helpView()
	}
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i := range lines {
		lines[i] = fit(displayText(lines[i]), m.width)
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	return view
}

func (m *Model) jobView() []string {
	lines := []string{m.projectHeader(), "", "JOB  SCHEDULE  NEXT  LAST  STATE"}
	if len(m.dashboard.Jobs) == 0 {
		lines = append(lines, "No jobs configured.")
	} else {
		rows := m.visibleRows(6)
		start := clamp(m.selectedJob-rows+1, 0, max(0, len(m.dashboard.Jobs)-rows))
		end := min(len(m.dashboard.Jobs), start+rows)
		for i := start; i < end; i++ {
			job := m.dashboard.Jobs[i]
			marker := "  "
			if i == m.selectedJob {
				marker = "> "
			}
			schedule := "manual"
			if job.Schedule != nil {
				schedule = *job.Schedule
			}
			next := "—"
			if job.Paused {
				next = "paused"
			} else if !job.Enabled {
				next = "disabled"
			} else if job.Schedule != nil && job.NextAt != nil {
				next = job.NextAt.Format("Mon 15:04")
			}
			last := "never"
			if job.LastRun != nil {
				last = job.LastRun.Status + " " + app.RelativeTime(job.LastRun.StartedAt, time.Now())
			}
			lines = append(lines, fmt.Sprintf("%s%-12s %-14s %-10s %-16s %s", marker, job.Name, schedule, next, last, jobState(m.dashboard.Project, job)))
		}
	}
	lines = append(lines, "", m.statusLine(), "j/k move · enter runs · l log · r run now · p pause · ? help · q quit")
	return lines
}

func (m *Model) runsView() []string {
	lines := []string{m.projectHeader(), fmt.Sprintf("Runs for %s · status: %s", m.runsJob, statusFilterNames[m.statusFilter]), "", "RUN  STATUS       TRIGGER  STARTED  DURATION  EXIT"}
	if len(m.runs) == 0 {
		lines = append(lines, "No matching runs.")
	} else {
		rows := m.visibleRows(7)
		start := clamp(m.selectedRun-rows+1, 0, max(0, len(m.runs)-rows))
		end := min(len(m.runs), start+rows)
		for i := start; i < end; i++ {
			run := m.runs[i]
			marker := "  "
			if i == m.selectedRun {
				marker = "> "
			}
			exit := "—"
			if run.ExitCode != nil {
				exit = strconv.Itoa(*run.ExitCode)
			}
			lines = append(lines, fmt.Sprintf("%s%-5d %-12s %-8s %-10s %-9s %s", marker, run.ID, run.Status, run.Trigger, app.RelativeTime(run.StartedAt, time.Now()), app.DurationText(run.DurationMS), exit))
		}
	}
	lines = append(lines, "", m.statusLine(), "j/k move · enter/l log · f filter · c cancel · r run now · p pause · esc back · ? help · q quit")
	return lines
}

func (m *Model) logView() []string {
	run := m.logRun
	lines := []string{
		m.projectHeader(),
		fmt.Sprintf("Run %d · %s · section: %s", run.ID, run.Status, m.logSection),
		"Log: " + logPath(run),
		"Hook: " + hookState(run),
		"",
	}
	logLines := splitLogLines(m.logText)
	visible := m.visibleRows(8)
	start := clamp(m.logScroll, 0, max(0, len(logLines)-visible))
	end := min(len(logLines), start+visible)
	lines = append(lines, logLines[start:end]...)
	for len(lines) < m.height-3 {
		lines = append(lines, "")
	}
	lines = append(lines, m.statusLine(), "j/k scroll · g/G top/bottom · / search · s section · esc back · ? help · q quit")
	if m.searchPrompt {
		lines[len(lines)-1] = "Search RE2: /" + m.searchInput + "_  · enter search · esc cancel"
	}
	return lines
}

func (m *Model) helpView() []string {
	return []string{
		"Noon terminal UI",
		"",
		"Jobs: j/k or arrows select; enter opens runs; l opens the latest log.",
		"Runs: j/k or arrows select; enter/l opens the log; f cycles status filters.",
		"Logs: j/k scroll; g/G jump to the first/last view; / searches with RE2 and 3 lines of context.",
		"Logs: s cycles job → hook → all; esc returns to the previous screen or search tail.",
		"Mutations: r queues the selected job now; p toggles pause; c asks before cancelling a run.",
		"The TUI never adds, edits, enables, or approves jobs.",
		"",
		m.statusLine(),
		"esc or ? back · q quit",
	}
}

func (m *Model) projectHeader() string {
	name := m.dashboard.Project.Name
	if name == "" {
		name = "project"
	}
	state := m.dashboard.Scheduler.State
	if state == "" {
		state = "offline"
	}
	return fmt.Sprintf("%s · scheduler %s · %s", name, state, m.dashboard.Project.Path)
}

func (m *Model) statusLine() string {
	parts := make([]string, 0, len(m.dashboard.Notices)+4)
	if m.confirmCancelID != 0 {
		parts = append(parts, fmt.Sprintf("Cancel run %d? Press y to confirm or n to keep it.", m.confirmCancelID))
	}
	if m.message != "" {
		parts = append(parts, m.message)
	}
	if m.logNotice != "" {
		parts = append(parts, m.logNotice)
	}
	if m.followNotice {
		parts = append(parts, "[noon: output past 1 MiB is shown when the run ends]")
	}
	for _, notice := range m.dashboard.Notices {
		parts = append(parts, noticeText(notice, m.dashboard.Next))
	}
	if m.searchPrompt {
		parts = append(parts, "Enter a Go RE2 pattern")
	}
	if len(parts) == 0 {
		return "Ready."
	}
	line := parts[0]
	for _, part := range parts[1:] {
		candidate := line + " · " + part
		if utf8.RuneCountInString(candidate) > m.width {
			break
		}
		line = candidate
	}
	return fit(line, m.width)
}

func noticeText(notice app.Notice, next string) string {
	switch notice.Code {
	case "not_enabled":
		return "Not enabled · noon enable"
	case "needs_approval":
		return "Needs approval · noon enable"
	case "scheduler_offline":
		return "Scheduler offline · " + next
	case "config_error":
		return "Config error · noon config check"
	default:
		return notice.Message
	}
}

func jobState(project app.DashboardProject, job app.Job) string {
	switch {
	case !job.Enabled:
		return "disabled"
	case job.Paused:
		return "paused"
	case job.Schedule != nil && !project.Enabled:
		return "not enabled"
	case job.Approval == "needs_approval":
		return "needs approval"
	default:
		return "ready"
	}
}

func logPath(run app.Run) string {
	if run.Log.Path == nil {
		return "unavailable"
	}
	return *run.Log.Path
}

func hookState(run app.Run) string {
	if run.Hook == nil {
		return "none"
	}
	value := run.Hook.Event + " " + run.Hook.Status
	if run.Hook.ExitCode != nil {
		value += " · exit " + strconv.Itoa(*run.Hook.ExitCode)
	}
	return value
}

func (m *Model) visibleRows(fixed int) int { return max(1, m.height-fixed) }

func (m *Model) refresh() {
	if err := m.refreshDashboard(); err != nil {
		m.message = displayText(err.Error())
	}
	if m.screen == runsScreen {
		m.refreshRuns()
	}
	if m.screen == logScreen {
		m.pollLog()
	}
}

func (m *Model) refreshDashboard() error {
	selected := m.selectedJobName
	view, err := app.Dashboard(m.input, time.Now())
	if err != nil {
		return err
	}
	m.dashboard = view
	m.selectedJob = 0
	if selected != "" {
		for i := range view.Jobs {
			if view.Jobs[i].Name == selected {
				m.selectedJob = i
				break
			}
		}
	}
	if len(view.Jobs) == 0 {
		m.selectedJobName = ""
	} else {
		m.selectedJob = clamp(m.selectedJob, 0, len(view.Jobs)-1)
		m.selectedJobName = view.Jobs[m.selectedJob].Name
	}
	return nil
}

func (m *Model) refreshRuns() {
	if m.runsJob == "" {
		m.runs = []app.Run{}
		return
	}
	previousID := m.selectedRunID
	runs, err := app.RunsWithStatuses(m.stateDir, m.project, m.runsJob, statusFilters[m.statusFilter], 50)
	if err != nil {
		m.message = displayText(err.Error())
		return
	}
	m.runs = runs
	m.selectedRun = 0
	if previousID != 0 {
		for i := range runs {
			if runs[i].ID == previousID {
				m.selectedRun = i
				break
			}
		}
	}
	if len(runs) == 0 {
		m.selectedRunID = 0
	} else {
		m.selectedRun = clamp(m.selectedRun, 0, len(runs)-1)
		m.selectedRunID = runs[m.selectedRun].ID
	}
}

func (m *Model) moveJob(delta int) {
	if len(m.dashboard.Jobs) == 0 {
		return
	}
	m.selectedJob = clamp(m.selectedJob+delta, 0, len(m.dashboard.Jobs)-1)
	m.selectedJobName = m.dashboard.Jobs[m.selectedJob].Name
	m.message = ""
}

func (m *Model) moveRun(delta int) {
	if len(m.runs) == 0 {
		return
	}
	m.selectedRun = clamp(m.selectedRun+delta, 0, len(m.runs)-1)
	m.selectedRunID = m.runs[m.selectedRun].ID
	m.message = ""
}

func (m *Model) openRuns() {
	job := m.currentJob()
	if job == nil {
		m.message = "No job selected."
		return
	}
	m.runsJob = job.Name
	m.statusFilter = 0
	m.selectedRunID = 0
	m.screen = runsScreen
	m.message = ""
	m.refreshRuns()
}

func (m *Model) openLastLog() {
	job := m.currentJob()
	if job == nil || job.LastRun == nil {
		m.message = "No run to read for the selected job."
		return
	}
	m.openLog(*job.LastRun, jobsScreen)
}

func (m *Model) openSelectedLog() {
	if len(m.runs) == 0 {
		m.message = "No run selected."
		return
	}
	m.openLog(m.runs[m.selectedRun], runsScreen)
}

func (m *Model) openLog(run app.Run, from screen) {
	section := "all"
	if (run.Status == "failed" || run.Status == "timed_out") && run.Log.HookOffset != nil {
		section = "job"
	}
	m.logReturn, m.screen = from, logScreen
	m.logRun = run
	m.logSection = section
	m.logSearch = ""
	m.searchPrompt = false
	m.searchInput = ""
	m.followNotice = false
	m.logNotice = ""
	m.autoScroll = true
	m.message = ""
	if !m.loadLogTail(true) {
		m.screen = from
	}
}

func (m *Model) loadLogTail(resetFollow bool) bool {
	rows := m.visibleRows(8)
	query := app.LogQuery{Section: m.logSection, Tail: &rows, LimitBytes: intPointer(logPageBytes)}
	detail, err := app.GetRun(context.Background(), m.stateDir, m.logRun.ID, 0, query)
	if err != nil {
		m.message = errorMessage(err)
		return false
	}
	m.logRun, m.logSize = detail.Run, detail.Size
	m.logText = detail.Log
	m.logCursor = detail.Next
	m.logNotice = runLogNotice(detail.Run, detail.Orphaned)
	m.logScroll = m.maxLogScroll()
	m.autoScroll = true
	if resetFollow {
		m.logFollowed = activeRun(detail.Run) || detail.Run.Status == "queued"
		m.followNotice = m.logFollowed && (m.logCursor >= maxFollowLogBytes-3 || detail.Run.Log.Truncated || detail.Size >= maxFollowLogBytes)
	} else if m.logFollowed && (detail.Run.Log.Truncated || detail.Size >= maxFollowLogBytes) {
		m.followNotice = true
	}
	return true
}

func (m *Model) loadLogTop() {
	if m.logSearch != "" {
		m.logSearch = ""
	}
	zero := int64(0)
	query := app.LogQuery{Section: m.logSection, After: &zero, LimitBytes: intPointer(logPageBytes)}
	detail, err := app.GetRun(context.Background(), m.stateDir, m.logRun.ID, 0, query)
	if err != nil {
		m.message = errorMessage(err)
		return
	}
	m.logRun, m.logSize, m.logText = detail.Run, detail.Size, detail.Log
	m.logCursor = detail.Next
	m.logNotice = runLogNotice(detail.Run, detail.Orphaned)
	m.logScroll, m.autoScroll = 0, false
}

func (m *Model) cycleLogSection() {
	sections := []string{"job", "hook", "all"}
	index := 0
	for i, section := range sections {
		if section == m.logSection {
			index = (i + 1) % len(sections)
			break
		}
	}
	m.logSection = sections[index]
	m.logSearch = ""
	m.followNotice = false
	m.message = ""
	m.loadLogTail(true)
}

func (m *Model) runSearch() {
	pattern := m.searchInput
	m.searchPrompt = false
	m.searchInput = ""
	if _, err := regexp.Compile(pattern); err != nil {
		m.message = fmt.Sprintf("invalid RE2 pattern: %v", err)
		return
	}
	contextLines := 3
	query := app.LogQuery{Section: m.logSection, Match: &pattern, Context: &contextLines, LimitBytes: intPointer(logPageBytes)}
	detail, err := app.GetRun(context.Background(), m.stateDir, m.logRun.ID, 0, query)
	if err != nil {
		m.message = errorMessage(err)
		return
	}
	m.logRun, m.logSize, m.logText = detail.Run, detail.Size, detail.Log
	m.logSearch = pattern
	m.logNotice = runLogNotice(detail.Run, detail.Orphaned)
	m.logScroll, m.autoScroll = 0, false
	if detail.MatchedLines != nil && *detail.MatchedLines == 0 {
		m.message = "No matches for /" + pattern + "/."
	} else {
		m.message = ""
	}
}

func (m *Model) pollLog() {
	if m.logRun.ID == 0 {
		return
	}
	previousActive := activeRun(m.logRun)
	wasFollowing := m.logFollowed
	limit := logPageBytes
	pausedAtCap := wasFollowing && previousActive && m.logCursor >= maxFollowLogBytes-3
	if wasFollowing && !pausedAtCap {
		remaining := maxFollowLogBytes - m.logCursor
		if remaining < 4 {
			if previousActive {
				pausedAtCap = true
			}
		} else {
			limit = int(min(int64(logPageBytes), remaining))
		}
	}
	if pausedAtCap {
		limit = 4 // Read status/orphan state at the cursor without advancing past the 1 MiB cap.
	}
	after := m.logCursor
	query := app.LogQuery{Section: m.logSection, After: &after, LimitBytes: &limit}
	detail, err := app.GetRun(context.Background(), m.stateDir, m.logRun.ID, 0, query)
	if err != nil {
		m.message = errorMessage(err)
		return
	}
	oldCursor := m.logCursor
	m.logRun, m.logSize = detail.Run, detail.Size
	m.logNotice = runLogNotice(detail.Run, detail.Orphaned)
	if activeRun(detail.Run) {
		m.logFollowed = true
	}
	if wasFollowing && (detail.Run.Log.Truncated || detail.Size >= maxFollowLogBytes) {
		m.followNotice = true
	}
	if activeRun(detail.Run) && pausedAtCap {
		m.followNotice = true
		return
	}
	if detail.Next > oldCursor {
		if m.logSearch == "" {
			m.appendLogText(detail.Log)
			if m.autoScroll {
				m.logScroll = m.maxLogScroll()
			}
		}
		m.logCursor = detail.Next
	}
	if m.logFollowed && m.logCursor >= maxFollowLogBytes-3 {
		m.followNotice = true
	}
	if detail.Orphaned {
		m.logFollowed = false
	}
}

func runLogNotice(run app.Run, orphaned bool) string {
	if orphaned {
		failure := app.OrphanedRunError(run.ID)
		return failure.Message + " · " + strings.TrimPrefix(failure.Hint, "Record it: ")
	}
	if run.Status == "queued" {
		return fmt.Sprintf("noon: run %d is queued; waiting for it to start", run.ID)
	}
	if !run.Log.Available {
		failure := app.MissingRunLogError(run)
		if failure.Hint != "" {
			return failure.Message + " · " + failure.Hint
		}
		return failure.Message
	}
	return ""
}

func activeRun(run app.Run) bool {
	return run.Status == "running" || (run.Hook != nil && run.Hook.Status == "running")
}

func (m *Model) askCancel() {
	var id int64
	if m.screen == logScreen {
		id = m.logRun.ID
	} else if len(m.runs) > 0 {
		id = m.runs[m.selectedRun].ID
	}
	if id == 0 {
		m.message = "No run selected."
		return
	}
	m.confirmCancelID = id
	m.message = ""
}

func (m *Model) cancelRun(id int64) {
	m.confirmCancelID = 0
	result, err := app.Cancel(m.stateDir, m.project, &id, "", time.Now())
	if err != nil {
		if app.IsMissingRun(err) {
			m.message = errorMessage(app.RunNotFoundError(id))
		} else {
			m.message = errorMessage(err)
		}
		return
	}
	switch {
	case result.Pending:
		m.message = fmt.Sprintf("Cancellation request recorded for run %d; it is still running.", id)
	case len(result.Runs) > 0 && result.Runs[0].Status == "cancelled" && result.Runs[0].StartedAt == nil:
		m.message = fmt.Sprintf("Cancelled queued run %d.", id)
	case len(result.Runs) > 0 && result.Runs[0].Status == "cancelled":
		m.message = fmt.Sprintf("Cancelled run %d.", id)
	case len(result.Runs) > 0:
		m.message = fmt.Sprintf("Run %d is already %s.", id, result.Runs[0].Status)
	default:
		m.message = fmt.Sprintf("Run %d cancellation requested.", id)
	}
	if m.screen == runsScreen {
		m.refreshRuns()
	}
}

func (m *Model) queueSelectedJob() {
	jobName := m.selectedActionJob()
	if jobName == "" {
		m.message = "No job selected."
		return
	}
	m.queueJob(jobName)
}

func (m *Model) queueJob(jobName string) {
	cfg, err := config.Load(m.input)
	if err != nil {
		m.message = errorMessage(app.ConfigError(err))
		return
	}
	now := time.Now()
	queued, err := app.QueueRun(cfg, jobName, "", now, now, time.Now, true, "cli", app.QueueOptions{EventSource: "cli"})
	if err != nil {
		m.message = errorMessage(err)
		return
	}
	run := queued.Run
	switch {
	case queued.Created:
		m.message = fmt.Sprintf("Queued %s as run %d · due now", run.Job, run.ID)
	case queued.Reason == "queued":
		m.message = fmt.Sprintf("%s is already queued as run %d · due now", run.Job, run.ID)
	case queued.Reason == "cooldown":
		m.message = fmt.Sprintf("%s is cooling down until %s (run %d)", run.Job, queued.CooldownUntil, run.ID)
	default:
		m.message = fmt.Sprintf("Already requested as run %d (%s)", run.ID, run.Status)
	}
	if run.Status == "queued" || run.Status == "running" {
		m.message += fmt.Sprintf(" · Follow it: noon logs %d --follow", run.ID)
	} else {
		m.message += fmt.Sprintf(" · Read it: noon logs %d", run.ID)
	}
	m.refreshDashboard()
	if m.screen == runsScreen {
		m.refreshRuns()
	}
}

func (m *Model) togglePause() {
	name := m.selectedActionJob()
	if name == "" {
		m.message = "No job selected."
		return
	}
	cfg, err := config.Load(m.input)
	if err != nil {
		m.message = errorMessage(app.ConfigError(err))
		return
	}
	if _, ok := cfg.Jobs[name]; !ok {
		m.message = fmt.Sprintf("job %q not found · Run noon pause to list jobs", name)
		return
	}
	if err := m.refreshDashboard(); err != nil {
		m.message = errorMessage(err)
		return
	}
	job, ok := m.configuredJob(name)
	if !ok {
		m.message = fmt.Sprintf("job %q not found · Run noon pause to list jobs", name)
		return
	}
	paused := !job.Paused
	if err := app.SetPaused(m.stateDir, m.project, name, paused, time.Now()); err != nil {
		m.message = errorMessage(err)
		return
	}
	if paused {
		m.message = fmt.Sprintf("Paused %s. Background runs stop until noon resume %s.", name, name)
	} else {
		m.message = fmt.Sprintf("Resumed %s. Its schedule starts from now.", name)
	}
	m.refreshDashboard()
}

func (m *Model) selectedActionJob() string {
	if m.screen == runsScreen {
		return m.runsJob
	}
	if m.screen == logScreen {
		return m.logRun.Job
	}
	if job := m.currentJob(); job != nil {
		return job.Name
	}
	return ""
}

func (m *Model) configuredJob(name string) (app.Job, bool) {
	for _, job := range m.dashboard.Jobs {
		if job.Name == name {
			return job, true
		}
	}
	return app.Job{}, false
}

func (m *Model) currentJob() *app.Job {
	if m.selectedJob < 0 || m.selectedJob >= len(m.dashboard.Jobs) {
		return nil
	}
	return &m.dashboard.Jobs[m.selectedJob]
}

func (m *Model) goBack() {
	switch m.screen {
	case logScreen:
		if m.logSearch != "" {
			m.logSearch = ""
			m.message = ""
			m.loadLogTail(true)
			return
		}
		m.screen = m.logReturn
	case runsScreen:
		m.screen = jobsScreen
	case helpScreen:
		m.screen = m.helpFrom
	}
}

func (m *Model) scrollLog(delta int) {
	m.logScroll = clamp(m.logScroll+delta, 0, m.maxLogScroll())
	m.autoScroll = m.logScroll == m.maxLogScroll()
}

func (m *Model) maxLogScroll() int {
	lines := splitLogLines(m.logText)
	return max(0, len(lines)-m.visibleRows(8))
}

func (m *Model) appendLogText(text string) {
	if text == "" {
		return
	}
	m.logText += text
	if len(m.logText) > maxLogBufferBytes {
		trim := len(m.logText) - maxLogBufferBytes
		if newline := strings.IndexByte(m.logText[trim:], '\n'); newline >= 0 {
			trim += newline + 1
		}
		m.logText = m.logText[trim:]
	}
}

func dropLastRune(value string) string {
	if value == "" {
		return value
	}
	_, size := utf8.DecodeLastRuneInString(value)
	return value[:len(value)-size]
}

func splitLogLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	for i := range lines {
		lines[i] = displayText(lines[i])
	}
	return lines
}

func displayText(value string) string {
	var output strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) {
			if r <= 0xff {
				fmt.Fprintf(&output, "\\x%02x", r)
			} else {
				fmt.Fprintf(&output, "\\u%04x", r)
			}
		} else {
			output.WriteRune(r)
		}
	}
	return output.String()
}

func fit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func clamp(value, low, high int) int {
	return min(max(value, low), high)
}

func intPointer(value int) *int { return &value }

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	var queueFailure *app.QueueError
	if errors.As(err, &queueFailure) {
		return withHint(queueFailure.Message, queueFailure.Hint)
	}
	var failure *app.Error
	if errors.As(err, &failure) {
		return withHint(failure.Message, failure.Hint)
	}
	return err.Error()
}

func withHint(message, hint string) string {
	if hint == "" {
		return message
	}
	return message + " · " + hint
}

// Package tui provides the optional terminal UI adapter for Noon.
package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
)

const (
	refreshInterval   = time.Second
	animateInterval   = 120 * time.Millisecond
	logPageBytes      = 64 << 10
	logTailLines      = 10000
	maxFollowLogBytes = 1 << 20
	maxLogBufferBytes = 2*maxFollowLogBytes + 128<<10
	searchContext     = 3
)

type screen int

const (
	jobsScreen screen = iota
	runsScreen
	logScreen
	helpScreen
)

type tone int

const (
	toneInfo tone = iota
	toneSuccess
	toneWarning
	toneError
)

type (
	refreshMsg struct{}
	animateMsg struct{}
)

// statusFilters cycle the runs view through NOON-26 status selections.
var statusFilters = []struct {
	label, title, empty string
	statuses            []string
}{
	{"all", "all runs", "No runs yet", nil},
	{"failures", "failed, timed out, or interrupted runs", "No failed, timed out, or interrupted runs", []string{"failed", "timed_out", "interrupted"}},
	{"active", "running or queued runs", "No running or queued runs", []string{"running", "queued"}},
}

var logSections = []string{"job", "hook", "all"}

// Model contains the TUI's view state. All state reads and mutations go
// through internal/app, the same operations used by the CLI and MCP clients.
type Model struct {
	input    config.Input
	stateDir string
	project  string
	home     string

	dashboard app.DashboardView
	screen    screen
	helpFrom  screen
	width     int
	height    int

	theme      theme
	profile    colorprofile.Profile
	dark       bool
	background color.Color
	spinFrame  int
	animating  bool

	selectedJob     int
	selectedJobName string
	jobOffset       int
	runsJob         string
	runs            []app.Run
	selectedRun     int
	selectedRunID   int64
	runOffset       int
	statusFilter    int
	helpScroll      int

	message         string
	messageTone     tone
	confirmCancelID int64

	logReturn         screen
	logRun            app.Run
	logSection        string
	logSearch         string
	searchRE          *regexp.Regexp
	searchPrompt      bool
	searchInput       []rune
	searchCursor      int
	matchCursor       int
	matchedLines      int
	logText           string
	logTimes          []app.LineTime
	logVersion        int
	logRowCache       logRowCache
	logPartial        bool
	logWrap           bool
	showLogTimestamps bool
	logCursor         int64
	logSize           int64
	logScroll         int
	logFollowed       bool
	logOrphaned       bool
	followNotice      bool
	autoScroll        bool

	logNotice     string
	logNoticeTone tone
}

// NewModel loads the current project dashboard without creating application
// state. Mutations are performed only by explicit keyboard actions.
func NewModel(input config.Input) (*Model, error) {
	cfg, err := app.LoadJobs(input, time.Now())
	if err != nil {
		return nil, app.ConfigError(err)
	}
	home, _ := os.UserHomeDir()
	model := &Model{
		input: input, stateDir: cfg.StateDir, project: cfg.Project, home: home,
		screen: jobsScreen, width: 80, height: 24, dark: true, logWrap: true, showLogTimestamps: true,
	}
	model.theme = newTheme(model.profile, model.dark, model.background)
	if err := model.refreshDashboard(); err != nil {
		return nil, err
	}
	return model, nil
}

func (m *Model) Init() tea.Cmd { return refreshTick() }

func refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshMsg{} })
}

func animateTick() tea.Cmd {
	return tea.Tick(animateInterval, func(time.Time) tea.Msg { return animateMsg{} })
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
		m.clampScroll()
		return m, nil
	case tea.ColorProfileMsg:
		m.profile = message.Profile
		m.theme = newTheme(m.profile, m.dark, m.background)
		if m.theme.mono {
			return m, nil
		}
		// Ask once the terminal can show color, so palettes match its background.
		return m, tea.RequestBackgroundColor
	case tea.BackgroundColorMsg:
		m.dark, m.background = message.IsDark(), message.Color
		m.theme = newTheme(m.profile, m.dark, m.background)
		return m, nil
	case refreshMsg:
		m.refresh()
		return m, tea.Batch(refreshTick(), m.startAnimation())
	case animateMsg:
		if !m.hasActivity() {
			m.animating = false
			return m, nil
		}
		m.spinFrame++
		return m, animateTick()
	case tea.PasteMsg:
		if m.searchPrompt {
			text, _, _ := strings.Cut(message.Content, "\n")
			m.insertSearch(strings.TrimSuffix(text, "\r"))
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.updateKey(message)
	default:
		return m, nil
	}
}

// startAnimation runs the spinner tick only while a visible run is active.
func (m *Model) startAnimation() tea.Cmd {
	if m.animating || !m.hasActivity() {
		return nil
	}
	m.animating = true
	return animateTick()
}

func (m *Model) hasActivity() bool {
	switch m.screen {
	case jobsScreen:
		for _, job := range m.dashboard.Jobs {
			if job.LastRun != nil && activeRun(*job.LastRun) {
				return true
			}
		}
	case runsScreen:
		for _, run := range m.runs {
			if activeRun(run) {
				return true
			}
		}
	case logScreen:
		return activeRun(m.logRun) && !m.logOrphaned
	}
	return false
}

// keyName normalizes terminals that report shifted letters as modifiers.
func keyName(message tea.KeyPressMsg) string {
	name := message.String()
	if rest, ok := strings.CutPrefix(name, "shift+"); ok && len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z' {
		return strings.ToUpper(rest)
	}
	return name
}

func (m *Model) updateKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := keyName(message)
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.searchPrompt {
		m.editSearch(message, key)
		return m, nil
	}
	if m.confirmCancelID != 0 {
		switch key {
		case "y", "Y":
			m.cancelRun(m.confirmCancelID)
		case "n", "N", "esc":
			m.flash(toneInfo, fmt.Sprintf("Kept run %d.", m.confirmCancelID))
			m.confirmCancelID = 0
		}
		return m, nil
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		if m.screen == helpScreen {
			m.screen = m.helpFrom
		} else {
			m.helpFrom, m.screen, m.helpScroll = m.screen, helpScreen, 0
		}
		return m, nil
	case "esc", "h", "left", "backspace":
		m.goBack()
		return m, nil
	}
	switch m.screen {
	case jobsScreen:
		m.jobsKey(key)
	case runsScreen:
		m.runsKey(key)
	case logScreen:
		m.logKey(key)
	case helpScreen:
		if delta, ok := listMove(key, m.helpBodyHeight()); ok {
			m.helpScroll = clamp(m.helpScroll+delta, 0, m.maxHelpScroll())
		}
	}
	return m, m.startAnimation()
}

// listMove maps vim, less, and arrow navigation keys to a cursor delta.
func listMove(key string, page int) (int, bool) {
	page = max(1, page)
	switch key {
	case "j", "down", "ctrl+n":
		return 1, true
	case "k", "up", "ctrl+p":
		return -1, true
	case "ctrl+d":
		return max(1, page/2), true
	case "ctrl+u":
		return -max(1, page/2), true
	case "ctrl+f", "pgdown":
		return page, true
	case "ctrl+b", "pgup":
		return -page, true
	case "g", "home":
		return -1 << 30, true
	case "G", "end":
		return 1 << 30, true
	}
	return 0, false
}

func (m *Model) jobsKey(key string) {
	if delta, ok := listMove(key, m.jobsLayout().rows); ok {
		m.moveJob(delta)
		return
	}
	switch key {
	case "enter", "right":
		m.openRuns()
	case "l":
		m.openLastLog()
	case "r":
		m.queueSelectedJob()
	case "p":
		m.togglePause()
	}
}

func (m *Model) runsKey(key string) {
	if delta, ok := listMove(key, m.runsLayout().rows); ok {
		m.moveRun(delta)
		return
	}
	switch key {
	case "enter", "right", "l":
		m.openSelectedLog()
	case "f", "tab":
		m.setStatusFilter(m.statusFilter + 1)
	case "shift+tab":
		m.setStatusFilter(m.statusFilter - 1)
	case "c":
		m.askCancel()
	case "r":
		m.queueSelectedJob()
	case "p":
		m.togglePause()
	}
}

func (m *Model) logKey(key string) {
	page := m.logBodyHeight()
	switch key {
	case "g", "home":
		m.loadLogTop()
		return
	case "G", "end":
		m.clearSearch()
		m.clearFlash()
		m.loadLogTail(false)
		return
	case "space":
		key = "pgdown"
	case "b":
		key = "pgup"
	case "ctrl+e":
		key = "down"
	case "ctrl+y":
		key = "up"
	}
	if delta, ok := listMove(key, page); ok {
		m.scrollLog(delta)
		return
	}
	switch key {
	case "/":
		m.searchPrompt, m.searchInput, m.searchCursor = true, nil, 0
		m.clearFlash()
	case "n":
		m.jumpMatch(1)
	case "N":
		m.jumpMatch(-1)
	case "s", "tab":
		m.cycleLogSection(1)
	case "shift+tab":
		m.cycleLogSection(-1)
	case "w":
		m.toggleWrap()
	case "t":
		m.toggleTimestamps()
	case "c":
		m.askCancel()
	case "r":
		m.queueSelectedJob()
	case "p":
		m.togglePause()
	}
}

// editSearch is a small readline-style editor for the RE2 prompt.
func (m *Model) editSearch(message tea.KeyPressMsg, key string) {
	input, cursor := m.searchInput, m.searchCursor
	switch key {
	case "enter":
		m.runSearch()
	case "esc":
		m.searchPrompt, m.searchInput, m.searchCursor = false, nil, 0
	case "backspace", "ctrl+h":
		if len(input) == 0 {
			m.searchPrompt = false
			return
		}
		if cursor > 0 {
			m.searchInput = append(input[:cursor-1:cursor-1], input[cursor:]...)
			m.searchCursor--
		}
	case "delete", "ctrl+d":
		if cursor < len(input) {
			m.searchInput = append(input[:cursor:cursor], input[cursor+1:]...)
		}
	case "left", "ctrl+b":
		m.searchCursor = max(0, cursor-1)
	case "right", "ctrl+f":
		m.searchCursor = min(len(input), cursor+1)
	case "home", "ctrl+a":
		m.searchCursor = 0
	case "end", "ctrl+e":
		m.searchCursor = len(input)
	case "ctrl+u":
		m.searchInput, m.searchCursor = append([]rune(nil), input[cursor:]...), 0
	case "ctrl+k":
		m.searchInput = input[:cursor:cursor]
	case "ctrl+w", "alt+backspace":
		start := cursor
		for start > 0 && input[start-1] == ' ' {
			start--
		}
		for start > 0 && input[start-1] != ' ' {
			start--
		}
		m.searchInput = append(input[:start:start], input[cursor:]...)
		m.searchCursor = start
	case "space":
		m.insertSearch(" ")
	default:
		if text := message.Key().Text; text != "" && message.Key().Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			m.insertSearch(text)
		}
	}
}

func (m *Model) insertSearch(text string) {
	runes := []rune(text)
	if len(runes) == 0 {
		return
	}
	input := make([]rune, 0, len(m.searchInput)+len(runes))
	input = append(input, m.searchInput[:m.searchCursor]...)
	input = append(input, runes...)
	input = append(input, m.searchInput[m.searchCursor:]...)
	m.searchInput = input
	m.searchCursor += len(runes)
}

func (m *Model) flash(kind tone, text string) {
	m.message, m.messageTone = text, kind
}

func (m *Model) clearFlash() { m.message = "" }

func (m *Model) refresh() {
	if err := m.refreshDashboard(); err != nil {
		m.flash(toneError, errorMessage(err))
	}
	if m.screen == runsScreen {
		m.refreshRuns()
	}
	if m.screen == logScreen && m.shouldPollLog() {
		m.pollLog()
	}
}

// shouldPollLog keeps reading runs that are or may become active. A finished
// run opened after it ended pages forward only as the reader nears the end of
// the loaded text, so idle views do not reread or trim the log.
func (m *Model) shouldPollLog() bool {
	if m.logRun.ID == 0 {
		return false
	}
	if m.logFollowed || activeRun(m.logRun) || m.logRun.Status == "queued" {
		return true
	}
	return m.wantsNextLogPage(m.logScroll)
}

func (m *Model) wantsNextLogPage(scroll int) bool {
	height := m.logBodyHeight()
	return m.logSearch == "" && m.logCursor < m.logSize && scroll+2*height >= len(m.logRows().rows)
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
	m.jobOffset = scrollOffset(m.jobOffset, m.selectedJob, len(view.Jobs), m.jobsLayout().rows)
	return nil
}

func (m *Model) refreshRuns() {
	if m.runsJob == "" {
		m.runs = []app.Run{}
		return
	}
	previousID := m.selectedRunID
	runs, err := app.RunsWithStatuses(m.stateDir, m.project, m.runsJob, statusFilters[m.statusFilter].statuses, 50)
	if err != nil {
		m.flash(toneError, errorMessage(err))
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
	m.runOffset = scrollOffset(m.runOffset, m.selectedRun, len(runs), m.runsLayout().rows)
}

// scrollOffset keeps a selection visible with a little context, moving the
// window only when the selection approaches an edge.
func scrollOffset(offset, selected, total, rows int) int {
	rows = max(1, rows)
	if total <= rows {
		return 0
	}
	margin := min(2, (rows-1)/2)
	if selected-margin < offset {
		offset = selected - margin
	}
	if selected+margin >= offset+rows {
		offset = selected + margin - rows + 1
	}
	return clamp(offset, 0, total-rows)
}

func (m *Model) clampScroll() {
	m.jobOffset = scrollOffset(m.jobOffset, m.selectedJob, len(m.dashboard.Jobs), m.jobsLayout().rows)
	m.runOffset = scrollOffset(m.runOffset, m.selectedRun, len(m.runs), m.runsLayout().rows)
	if m.autoScroll {
		m.logScroll = m.maxLogScroll()
	}
	m.logScroll = clamp(m.logScroll, 0, m.maxLogScroll())
	m.helpScroll = clamp(m.helpScroll, 0, m.maxHelpScroll())
}

func (m *Model) moveJob(delta int) {
	if len(m.dashboard.Jobs) == 0 {
		return
	}
	m.selectedJob = clamp(m.selectedJob+delta, 0, len(m.dashboard.Jobs)-1)
	m.selectedJobName = m.dashboard.Jobs[m.selectedJob].Name
	m.jobOffset = scrollOffset(m.jobOffset, m.selectedJob, len(m.dashboard.Jobs), m.jobsLayout().rows)
	m.clearFlash()
}

func (m *Model) moveRun(delta int) {
	if len(m.runs) == 0 {
		return
	}
	m.selectedRun = clamp(m.selectedRun+delta, 0, len(m.runs)-1)
	m.selectedRunID = m.runs[m.selectedRun].ID
	m.runOffset = scrollOffset(m.runOffset, m.selectedRun, len(m.runs), m.runsLayout().rows)
	m.clearFlash()
}

func (m *Model) setStatusFilter(index int) {
	m.statusFilter = (index%len(statusFilters) + len(statusFilters)) % len(statusFilters)
	m.selectedRunID, m.runOffset = 0, 0
	m.clearFlash()
	m.refreshRuns()
}

func (m *Model) openRuns() {
	job := m.currentJob()
	if job == nil {
		m.flash(toneInfo, "No job selected.")
		return
	}
	m.runsJob = job.Name
	m.statusFilter = 0
	m.selectedRunID, m.runOffset = 0, 0
	m.screen = runsScreen
	m.clearFlash()
	m.refreshRuns()
}

func (m *Model) openLastLog() {
	job := m.currentJob()
	if job == nil || job.LastRun == nil {
		m.flash(toneInfo, "No runs yet for this job · r queues one now.")
		return
	}
	m.openLog(*job.LastRun, jobsScreen)
}

func (m *Model) openSelectedLog() {
	if len(m.runs) == 0 {
		m.flash(toneInfo, "No run selected.")
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
	m.clearSearch()
	m.searchPrompt = false
	m.searchInput, m.searchCursor = nil, 0
	m.followNotice = false
	m.logNotice, m.logOrphaned = "", false
	m.autoScroll = true
	m.clearFlash()
	if !m.loadLogTail(true) {
		m.screen = from
		return
	}
	if section == "job" {
		m.flash(toneInfo, "Showing job output · s shows the hook's output")
	}
}

func (m *Model) setLogText(text string, times []app.LineTime) {
	m.logText = text
	m.logTimes = append(m.logTimes[:0], times...)
	m.logVersion++
}

func (m *Model) clearSearch() {
	m.logSearch, m.searchRE, m.matchCursor, m.matchedLines = "", nil, 0, 0
}

// loadLogTail reads up to one page of the newest whole lines, so the view
// opens on the last screenful with recent scrollback above it.
func (m *Model) loadLogTail(resetFollow bool) bool {
	tail := logTailLines
	query := app.LogQuery{Section: m.logSection, Tail: &tail, LimitBytes: intPointer(logPageBytes), Timestamps: true}
	detail, err := app.GetRun(context.Background(), m.stateDir, m.logRun.ID, 0, query)
	if err != nil {
		m.flash(toneError, errorMessage(err))
		return false
	}
	m.logRun, m.logSize = detail.Run, detail.Size
	m.setLogText(detail.Log, detailLineTimes(detail))
	m.logPartial = detail.Truncated || strings.Count(detail.Log, "\n") >= logTailLines
	m.logCursor = detail.Next
	m.setLogNotice(detail.Run, detail.Orphaned)
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
	m.clearSearch()
	m.clearFlash()
	zero := int64(0)
	query := app.LogQuery{Section: m.logSection, After: &zero, LimitBytes: intPointer(logPageBytes), Timestamps: true}
	detail, err := app.GetRun(context.Background(), m.stateDir, m.logRun.ID, 0, query)
	if err != nil {
		m.flash(toneError, errorMessage(err))
		return
	}
	m.logRun, m.logSize = detail.Run, detail.Size
	m.setLogText(detail.Log, detailLineTimes(detail))
	m.logPartial = false
	m.logCursor = detail.Next
	m.setLogNotice(detail.Run, detail.Orphaned)
	m.logScroll, m.autoScroll = 0, false
}

func (m *Model) cycleLogSection(step int) {
	if m.logRun.Log.HookOffset == nil {
		m.flash(toneInfo, "This run has no hook output; its log is all job output.")
		return
	}
	index := 0
	for i, section := range logSections {
		if section == m.logSection {
			index = (i + step + len(logSections)) % len(logSections)
			break
		}
	}
	m.logSection = logSections[index]
	m.clearSearch()
	m.followNotice = false
	m.clearFlash()
	if m.loadLogTail(true) {
		m.flash(toneInfo, "Showing "+m.logSection+" output")
	}
}

func (m *Model) toggleWrap() {
	m.logWrap = !m.logWrap
	if m.logWrap {
		m.flash(toneInfo, "Wrapping long lines.")
	} else {
		m.flash(toneInfo, "Long lines are cut at the window edge · w wraps them.")
	}
	m.clampScroll()
}

func (m *Model) toggleTimestamps() {
	previous := m.logRows()
	scroll := m.logScroll
	if m.autoScroll {
		scroll = max(0, len(previous.rows)-m.logBodyHeight())
	}
	anchor := logRow{line: -1}
	if scroll < len(previous.rows) {
		anchor = previous.rows[scroll]
	}
	matchOffset := -1
	if len(previous.matches) > 0 {
		matchOffset = previous.matches[clamp(m.matchCursor, 0, len(previous.matches)-1)] - scroll
	}

	m.showLogTimestamps = !m.showLogTimestamps
	current := m.logRows()
	switch {
	case m.autoScroll:
		m.logScroll = max(0, len(current.rows)-m.logBodyHeight())
	case matchOffset >= 0 && len(current.matches) > 0:
		matchRow := current.matches[clamp(m.matchCursor, 0, len(current.matches)-1)]
		relative := clamp(matchOffset, 0, max(0, m.logBodyHeight()-1))
		m.logScroll = matchRow - relative
	case anchor.line >= 0:
		for i, row := range current.rows {
			if row.line != anchor.line {
				continue
			}
			if anchor.start == row.start || row.start <= anchor.start && anchor.start < row.end {
				m.logScroll = i
				break
			}
		}
	}
	m.clampScroll()
	if m.showLogTimestamps {
		m.flash(toneInfo, "Log timestamps on.")
	} else {
		m.flash(toneInfo, "Log timestamps off.")
	}
}

func detailLineTimes(detail app.RunDetail) []app.LineTime {
	if detail.LineTimes == nil {
		return nil
	}
	return *detail.LineTimes
}

func (m *Model) runSearch() {
	pattern := string(m.searchInput)
	m.searchPrompt = false
	m.searchInput, m.searchCursor = nil, 0
	if pattern == "" {
		return
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		m.flash(toneError, fmt.Sprintf("Invalid RE2 pattern: %v", err))
		return
	}
	contextLines := searchContext
	query := app.LogQuery{Section: m.logSection, Match: &pattern, Context: &contextLines, LimitBytes: intPointer(logPageBytes), Timestamps: true}
	detail, err := app.GetRun(context.Background(), m.stateDir, m.logRun.ID, 0, query)
	if err != nil {
		m.flash(toneError, errorMessage(err))
		return
	}
	m.logRun, m.logSize = detail.Run, detail.Size
	m.setLogText(detail.Log, detailLineTimes(detail))
	m.logPartial = detail.Truncated
	m.logSearch, m.searchRE, m.matchCursor = pattern, compiled, 0
	m.matchedLines = 0
	if detail.MatchedLines != nil {
		m.matchedLines = *detail.MatchedLines
	}
	m.setLogNotice(detail.Run, detail.Orphaned)
	m.logScroll, m.autoScroll = 0, false
	if m.matchedLines == 0 {
		m.flash(toneWarning, "No matches for /"+pattern+"/.")
	} else {
		m.clearFlash()
		m.revealMatch()
	}
}

// jumpMatch moves between matching lines in search results, wrapping at the ends.
func (m *Model) jumpMatch(step int) {
	if m.logSearch == "" {
		m.flash(toneInfo, "No search · / searches this log.")
		return
	}
	matches := m.logRows().matches
	if len(matches) == 0 {
		return
	}
	next := m.matchCursor + step
	switch {
	case next >= len(matches):
		next = 0
		m.flash(toneInfo, "Search wrapped to the first match.")
	case next < 0:
		next = len(matches) - 1
		m.flash(toneInfo, "Search wrapped to the last match.")
	default:
		m.clearFlash()
	}
	m.matchCursor = next
	m.revealMatch()
}

// revealMatch scrolls the current match into the upper third of the view.
func (m *Model) revealMatch() {
	matches := m.logRows().matches
	if len(matches) == 0 {
		return
	}
	m.matchCursor = clamp(m.matchCursor, 0, len(matches)-1)
	row, height := matches[m.matchCursor], m.logBodyHeight()
	if row < m.logScroll || row >= m.logScroll+height {
		m.logScroll = clamp(row-height/3, 0, m.maxLogScroll())
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
	query := app.LogQuery{Section: m.logSection, After: &after, LimitBytes: &limit, Timestamps: true}
	detail, err := app.GetRun(context.Background(), m.stateDir, m.logRun.ID, 0, query)
	if err != nil {
		m.flash(toneError, errorMessage(err))
		return
	}
	oldCursor := m.logCursor
	m.logRun, m.logSize = detail.Run, detail.Size
	m.setLogNotice(detail.Run, detail.Orphaned)
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
			m.appendLogText(detail.Log, detailLineTimes(detail))
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

// setLogNotice records the shared queued, orphaned, or missing-log notice.
func (m *Model) setLogNotice(run app.Run, orphaned bool) {
	m.logOrphaned = orphaned
	switch {
	case orphaned:
		failure := app.OrphanedRunError(run.ID)
		m.logNotice, m.logNoticeTone = failure.Message+" · "+strings.TrimPrefix(failure.Hint, "Record it: "), toneError
	case run.Status == "queued":
		m.logNotice, m.logNoticeTone = fmt.Sprintf("noon: run %d is queued; waiting for it to start", run.ID), toneInfo
	case !run.Log.Available:
		failure := app.MissingRunLogError(run)
		m.logNotice, m.logNoticeTone = withHint(failure.Message, failure.Hint), toneWarning
	default:
		m.logNotice = ""
	}
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
		m.flash(toneInfo, "No run selected.")
		return
	}
	m.confirmCancelID = id
	m.clearFlash()
}

func (m *Model) cancelRun(id int64) {
	m.confirmCancelID = 0
	result, err := app.Cancel(m.stateDir, m.project, &id, "", time.Now())
	if err != nil {
		if app.IsMissingRun(err) {
			m.flash(toneError, errorMessage(app.RunNotFoundError(id)))
		} else {
			m.flash(toneError, errorMessage(err))
		}
		return
	}
	switch {
	case result.Pending:
		m.flash(toneSuccess, fmt.Sprintf("Cancellation request recorded for run %d; it is still running.", id))
	case len(result.Runs) > 0 && result.Runs[0].Status == "cancelled" && result.Runs[0].StartedAt == nil:
		m.flash(toneSuccess, fmt.Sprintf("Cancelled queued run %d.", id))
	case len(result.Runs) > 0 && result.Runs[0].Status == "cancelled":
		m.flash(toneSuccess, fmt.Sprintf("Cancelled run %d.", id))
	case len(result.Runs) > 0:
		m.flash(toneInfo, fmt.Sprintf("Run %d is already %s.", id, statusLabel(result.Runs[0].Status)))
	default:
		m.flash(toneSuccess, fmt.Sprintf("Run %d cancellation requested.", id))
	}
	if m.screen == runsScreen {
		m.refreshRuns()
	}
}

func (m *Model) queueSelectedJob() {
	jobName := m.selectedActionJob()
	if jobName == "" {
		m.flash(toneInfo, "No job selected.")
		return
	}
	m.queueJob(jobName)
}

func (m *Model) queueJob(jobName string) {
	cfg, err := app.LoadJobs(m.input, time.Now())
	if err != nil {
		m.flash(toneError, errorMessage(app.ConfigError(err)))
		return
	}
	now := time.Now()
	queued, err := app.QueueRun(cfg, jobName, "", now, now, time.Now, true, "cli", app.QueueOptions{EventSource: "cli"})
	if err != nil {
		m.flash(toneError, errorMessage(err))
		return
	}
	run := queued.Run
	// The first sentence matches `noon run --detach`; the rest points at TUI keys.
	switch {
	case queued.Created:
		m.flash(toneSuccess, fmt.Sprintf("Queued %s as run %d · due now", run.Job, run.ID))
	case queued.Reason == "queued":
		m.flash(toneInfo, fmt.Sprintf("%s is already queued as run %d · due now", run.Job, run.ID))
	case queued.Reason == "cooldown":
		wait := app.RelativeTime(&queued.CooldownUntil, now)
		m.flash(toneWarning, fmt.Sprintf("%s is cooling down (run %d) · it can run again %s", run.Job, run.ID, wait))
	default:
		m.flash(toneInfo, fmt.Sprintf("Already requested as run %d (%s)", run.ID, statusLabel(run.Status)))
	}
	active := run.Status == "queued" || run.Status == "running"
	filter := statusFilters[m.statusFilter].statuses
	selected := m.screen == runsScreen && active && (filter == nil || slices.Contains(filter, run.Status))
	if selected {
		m.selectedRunID = run.ID
	}
	switch {
	case run.Status == "queued" && m.dashboard.Scheduler.State != "running":
		m.message += " · it waits for the scheduler: " + m.schedulerFix()
	case active && m.screen == jobsScreen:
		m.message += " · l follows it"
	case selected:
		m.message += " · enter follows it"
	}
	if err := m.refreshDashboard(); err != nil {
		m.flash(toneError, errorMessage(err))
	}
	if m.screen == runsScreen {
		m.refreshRuns()
	}
}

func (m *Model) togglePause() {
	name := m.selectedActionJob()
	if name == "" {
		m.flash(toneInfo, "No job selected.")
		return
	}
	cfg, err := app.LoadJobs(m.input, time.Now())
	if err != nil {
		m.flash(toneError, errorMessage(app.ConfigError(err)))
		return
	}
	if _, ok := cfg.Jobs[name]; !ok {
		m.flash(toneError, fmt.Sprintf("job %q not found · Run noon pause to list jobs", name))
		return
	}
	if err := m.refreshDashboard(); err != nil {
		m.flash(toneError, errorMessage(err))
		return
	}
	job, ok := m.configuredJob(name)
	if !ok {
		m.flash(toneError, fmt.Sprintf("job %q not found · Run noon pause to list jobs", name))
		return
	}
	paused := !job.Paused
	if err := app.SetPaused(m.stateDir, m.project, name, paused, time.Now()); err != nil {
		m.flash(toneError, errorMessage(err))
		return
	}
	if paused {
		m.flash(toneSuccess, fmt.Sprintf("Paused %s · background runs stop until you resume it with p.", name))
	} else {
		m.flash(toneSuccess, fmt.Sprintf("Resumed %s · its schedule starts from now.", name))
	}
	if err := m.refreshDashboard(); err != nil {
		m.flash(toneError, errorMessage(err))
	}
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
			m.clearSearch()
			m.clearFlash()
			m.loadLogTail(true)
			return
		}
		m.screen = m.logReturn
		m.clearFlash()
		if m.screen == runsScreen {
			m.refreshRuns()
		}
	case runsScreen:
		m.screen = jobsScreen
		m.clearFlash()
	case helpScreen:
		m.screen = m.helpFrom
	}
}

// scrollLog moves through the loaded text, first reading the next page of a
// finished log when the move nears its end.
func (m *Model) scrollLog(delta int) {
	target := m.logScroll + delta
	if delta > 0 && !activeRun(m.logRun) && m.wantsNextLogPage(target) {
		m.autoScroll = false
		m.pollLog()
	}
	limit := m.maxLogScroll()
	m.logScroll = clamp(target, 0, limit)
	m.autoScroll = m.logScroll == limit
}

func (m *Model) maxLogScroll() int {
	return max(0, len(m.logRows().rows)-m.logBodyHeight())
}

func (m *Model) appendLogText(text string, times []app.LineTime) {
	if text == "" {
		return
	}
	oldText := m.logText
	newTimes := append([]app.LineTime(nil), times...)
	if oldText != "" && !strings.HasSuffix(oldText, "\n") && len(newTimes) > 0 {
		newTimes = newTimes[1:]
	}
	allTimes := append(append([]app.LineTime(nil), m.logTimes...), newTimes...)
	buffer := oldText + text
	if len(buffer) > maxLogBufferBytes {
		trim := len(buffer) - maxLogBufferBytes
		if newline := strings.IndexByte(buffer[trim:], '\n'); newline >= 0 {
			trim += newline + 1
		}
		drop := min(strings.Count(buffer[:trim], "\n"), len(allTimes))
		allTimes = allTimes[drop:]
		buffer = buffer[trim:]
		m.logPartial = true
	}
	m.setLogText(buffer, allTimes)
}

func splitLogLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
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

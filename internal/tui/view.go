package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/brettinternet/noon/internal/app"
)

var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// View renders a shared header, the current screen, and a status line. Every
// row is fitted to the window.
func (m *Model) View() tea.View {
	now := time.Now()
	rows := make([]string, 0, m.height)
	rows = append(rows, m.titleRow())
	if m.showBanner() {
		rows = append(rows, m.bannerRow())
	}
	if m.showGap() {
		rows = append(rows, "")
	}
	top, bottom := m.frame()
	height := max(0, m.height-top-bottom)
	var body []string
	switch m.screen {
	case jobsScreen:
		body = m.jobsBody(now)
	case runsScreen:
		body = m.runsBody(now)
	case logScreen:
		body = m.logBody(now)
	case helpScreen:
		body = m.helpBody()
	}
	for i := range height {
		if i < len(body) {
			rows = append(rows, body[i])
		} else {
			rows = append(rows, "")
		}
	}
	statusY := len(rows)
	rows = append(rows, m.statusRow())
	rows = rows[:min(len(rows), m.height)]
	for i, row := range rows {
		if ansi.StringWidth(row) > m.width {
			rows[i] = ansi.Truncate(row, m.width, "")
		}
	}
	view := tea.NewView(strings.Join(rows, "\n"))
	view.AltScreen = true
	if m.searchPrompt && statusY < m.height {
		_, x := m.promptText()
		view.Cursor = tea.NewCursor(x, statusY)
		view.Cursor.Shape = tea.CursorBar
	}
	return view
}

// frame returns the rows taken by the shared header and the status line.
func (m *Model) frame() (top, bottom int) {
	top = 1
	if m.showBanner() {
		top++
	}
	if m.showGap() {
		top++
	}
	return top, 1
}

func (m *Model) showBanner() bool { return m.height >= 12 && len(m.bannerItems()) > 0 }
func (m *Model) showGap() bool    { return m.height >= 16 }

// listLayout describes the rows of a jobs or runs screen below its title.
type listLayout struct {
	gap    bool // blank row between the screen title and the table
	rows   int  // visible table rows
	detail bool // a blank row and two detail rows follow the table
}

func (m *Model) listLayout() listLayout {
	top, bottom := m.frame()
	available := m.height - top - bottom - 2 // screen title and table header
	layout := listLayout{}
	if available >= 10 {
		layout.gap = true
		available--
	}
	if available >= 11 {
		layout.detail = true
		available -= 3
	}
	layout.rows = max(1, available)
	return layout
}

func (m *Model) jobsLayout() listLayout { return m.listLayout() }
func (m *Model) runsLayout() listLayout { return m.listLayout() }

// Header

func (m *Model) titleRow() string {
	t := m.theme
	name := m.dashboard.Project.Name
	if name == "" {
		name = "project"
	}
	left := []span{{" ", style{}}, {"noon", t.key}, {"  ", style{}}, {name, t.strong}}
	var right []span
	switch {
	case m.dashboard.Scheduler.State == "running":
		right = []span{{"● ", t.success}, {"scheduler running", t.muted}}
	case m.hasNotice("scheduler_offline"):
		right = []span{{"○ scheduler offline", t.warning}}
		if fix := (span{"  " + m.schedulerFix(), t.cmd}); spansWidth(left)+spansWidth(right)+spansWidth([]span{fix})+3 <= m.width {
			right = append(right, fix)
		}
	default:
		right = []span{{"○ ", t.faint}, {"scheduler offline", t.muted}}
	}
	right = append(right, span{" ", style{}})
	// The path matters least, so it takes only the room the rest leaves.
	if path := m.homePath(m.dashboard.Project.Path); path != "" {
		if room := m.width - spansWidth(left) - spansWidth(right) - 4; room >= 12 {
			left = append(left, span{"  " + truncateLeft(path, room), t.muted})
		}
	}
	return justify(m.width, style{}, left, right)
}

type bannerItem struct {
	icon, text, fix string
	tone            style
}

// bannerItems lists project notices with the CLI command that fixes each.
// The title shows scheduler state and the jobs table shows queued runs.
func (m *Model) bannerItems() []bannerItem {
	t := m.theme
	var items []bannerItem
	for _, notice := range m.dashboard.Notices {
		switch notice.Code {
		case "config_error":
			items = append(items, bannerItem{"✗", strings.TrimSuffix(notice.Message, "."), "noon config check", t.failure})
		case "not_enabled":
			items = append(items, bannerItem{"▲", "Not enabled for background runs", "noon enable", t.warning})
		case "needs_approval":
			items = append(items, bannerItem{"▲", m.approvalText(), "noon enable", t.warning})
		case "scheduler_offline", "queued":
		default:
			items = append(items, bannerItem{"•", strings.TrimSuffix(notice.Message, "."), "", t.muted})
		}
	}
	return items
}

func (m *Model) bannerRow() string {
	spans := []span{{" ", style{}}}
	for i, item := range m.bannerItems() {
		if i > 0 {
			spans = append(spans, span{"    ", style{}})
		}
		spans = append(spans, span{item.icon + " " + item.text, item.tone})
		if item.fix != "" {
			spans = append(spans, span{"  " + item.fix, m.theme.cmd})
		}
	}
	return fitSpans(spans, m.width, style{}, false)
}

func (m *Model) approvalText() string {
	count := 0
	for _, job := range m.dashboard.Jobs {
		if job.Enabled && job.Approval == "needs_approval" {
			count++
		}
	}
	if count == 1 {
		return "1 shared job needs approval"
	}
	return fmt.Sprintf("%d shared jobs need approval", count)
}

func (m *Model) hasNotice(code string) bool {
	for _, notice := range m.dashboard.Notices {
		if notice.Code == code {
			return true
		}
	}
	return false
}

// schedulerFix names the command that starts background work, even when a
// higher-priority notice owns the dashboard's single next step.
func (m *Model) schedulerFix() string {
	if next := m.dashboard.Next; next == "noon serve" || next == "noon service install" {
		return next
	}
	if m.dashboard.Service.Installed {
		return "noon serve"
	}
	return "noon service install"
}

func (m *Model) homePath(path string) string {
	if m.home != "" && (path == m.home || strings.HasPrefix(path, m.home+string(filepath.Separator))) {
		return "~" + path[len(m.home):]
	}
	return path
}

// breadcrumb renders the screen path, emphasizing the current location.
func (m *Model) breadcrumb(parts ...string) []span {
	t := m.theme
	spans := []span{{" ", style{}}}
	for i, part := range parts {
		if i > 0 {
			spans = append(spans, span{" › ", t.faint})
		}
		if i == len(parts)-1 {
			spans = append(spans, span{part, t.strong})
		} else {
			spans = append(spans, span{part, t.muted})
		}
	}
	return spans
}

func (m *Model) tabs(labels []string, active int) []span {
	spans := make([]span, 0, len(labels)+1)
	for i, label := range labels {
		if i == active {
			spans = append(spans, span{" " + label + " ", m.theme.tabActive})
		} else {
			spans = append(spans, span{" " + label + " ", m.theme.tab})
		}
	}
	return append(spans, span{" ", style{}})
}

// Tables

// column describes one table column. When a table is too wide, ranked
// columns give up room, highest rank first: each shrinks to its minimum and
// is then hidden. Unranked columns identify the row and shrink last.
type column struct {
	title    string
	min, max int
	rank     int
	right    bool
}

const columnGap = 3

type table struct {
	columns []column
	widths  []int
}

// newTable sizes columns to their content within width, after the two-cell
// selection marker and the margins on both sides.
func newTable(columns []column, cells [][][]span, width int) table {
	available := width - 4
	widths := make([]int, len(columns))
	for i, c := range columns {
		widths[i] = ansi.StringWidth(c.title)
		for _, row := range cells {
			widths[i] = max(widths[i], spansWidth(row[i]))
		}
		widths[i] = min(widths[i], c.max)
	}
	total := func() int {
		sum, count := 0, 0
		for _, w := range widths {
			if w > 0 {
				sum += w
				count++
			}
		}
		return sum + columnGap*max(0, count-1)
	}
	for total() > available {
		pick := -1
		for i, c := range columns {
			if widths[i] > 0 && c.rank > 0 && (pick < 0 || c.rank > columns[pick].rank) {
				pick = i
			}
		}
		if pick < 0 {
			break
		}
		if widths[pick] > columns[pick].min {
			widths[pick] = max(columns[pick].min, widths[pick]-(total()-available))
		} else {
			widths[pick] = 0
		}
	}
	for total() > available {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= 1 {
			break
		}
		widths[widest]--
	}
	return table{columns: columns, widths: widths}
}

func (tb table) header(t theme, width int) string {
	cells := make([][]span, len(tb.columns))
	for i, c := range tb.columns {
		cells[i] = []span{{c.title, t.faint}}
	}
	return tb.row(cells, style{}, span{" ", style{}}, width)
}

func (tb table) row(cells [][]span, fill style, marker span, width int) string {
	var out strings.Builder
	out.WriteString(fill.render(" "))
	out.WriteString(marker.style.over(fill).render(marker.text))
	out.WriteString(fill.render(" "))
	used, first := 3, true
	for i, w := range tb.widths {
		if w == 0 {
			continue
		}
		if !first {
			out.WriteString(fill.render(strings.Repeat(" ", columnGap)))
			used += columnGap
		}
		first = false
		out.WriteString(fitSpans(cells[i], w, fill, tb.columns[i].right))
		used += w
	}
	if used < width {
		out.WriteString(fill.render(strings.Repeat(" ", width-used)))
	}
	return out.String()
}

func (m *Model) tableRows(tb table, cells [][][]span, selected, offset, rows int) []string {
	t := m.theme
	out := make([]string, 0, rows+1)
	out = append(out, tb.header(t, m.width))
	for i := offset; i < min(len(cells), offset+rows); i++ {
		if i == selected {
			out = append(out, tb.row(cells[i], t.selected, span{"▎", t.marker}, m.width))
		} else {
			out = append(out, tb.row(cells[i], style{}, span{" ", style{}}, m.width))
		}
	}
	return out
}

func (m *Model) emptyRow(spans ...span) string {
	return fitSpans(append([]span{{"   ", style{}}}, spans...), m.width, style{}, false)
}

// Jobs

var jobColumns = []column{
	{title: "JOB", max: 28},
	{title: "SCHEDULE", min: 10, max: 26, rank: 3},
	{title: "NEXT", min: 8, max: 16, rank: 1},
	{title: "LAST RUN", max: 13},
	{title: "", min: 7, max: 12, rank: 2},
}

func (m *Model) jobsBody(now time.Time) []string {
	t := m.theme
	layout := m.jobsLayout()
	jobs := m.dashboard.Jobs
	title := append(m.breadcrumb("jobs"), span{"  " + strconv.Itoa(len(jobs)), t.faint})
	rows := []string{justify(m.width, style{}, title, m.jobsSummary())}
	if layout.gap {
		rows = append(rows, "")
	}
	if len(jobs) == 0 {
		if m.dashboard.Project.ConfigError != "" {
			return append(rows, m.emptyRow(span{"Jobs could not be loaded", t.muted}, span{"  noon config check", t.cmd}))
		}
		return append(rows, m.emptyRow(span{"No jobs configured", t.muted}, span{"  noon add", t.cmd}))
	}
	cells := make([][][]span, len(jobs))
	for i, job := range jobs {
		cells[i] = m.jobCells(job, now)
	}
	rows = append(rows, m.tableRows(newTable(jobColumns, cells, m.width), cells, m.selectedJob, m.jobOffset, layout.rows)...)
	if job := m.currentJob(); layout.detail && job != nil {
		rows = append(rows, "")
		rows = append(rows, m.jobDetail(*job)...)
	}
	return rows
}

// jobsSummary counts jobs that need attention, for scanning at a glance.
func (m *Model) jobsSummary() []span {
	t := m.theme
	var running, queued, failing, paused int
	for _, job := range m.dashboard.Jobs {
		if job.Paused {
			paused++
		}
		switch {
		case job.LastRun == nil:
		case activeRun(*job.LastRun):
			running++
		case job.LastRun.Status == "queued":
			queued++
		case failedStatus(job.LastRun.Status):
			failing++
		}
	}
	var spans []span
	add := func(count int, icon string, tone style, label string) {
		if count > 0 {
			spans = append(spans, span{icon + " ", tone}, span{fmt.Sprintf("%d %s", count, label), t.muted}, span{"   ", style{}})
		}
	}
	add(running, m.spinner(), t.running, "running")
	add(queued, "◌", t.queued, "queued")
	add(failing, "✗", t.failure, "failing")
	add(paused, "‖", t.warning, "paused")
	if len(spans) > 0 {
		spans[len(spans)-1].text = " "
	}
	return spans
}

func (m *Model) jobCells(job app.Job, now time.Time) [][]span {
	t := m.theme
	schedule := span{"manual", t.faint}
	if job.Schedule != nil {
		schedule = span{*job.Schedule, t.text}
	}
	status, when := m.lastRunCells(job.LastRun, now)
	return [][]span{{{job.Name, t.text}}, {schedule}, m.nextCell(job, now), status, when}
}

// nextCell shows when a job runs next, or the state that keeps it from running.
func (m *Model) nextCell(job app.Job, now time.Time) []span {
	t := m.theme
	switch {
	case !job.PlatformSupported:
		return []span{{"other platform", t.warning}}
	case !job.Enabled:
		return []span{{"disabled", t.faint}}
	case job.Paused:
		return []span{{"paused", t.warning}}
	case job.Schedule != nil && !m.dashboard.Project.Enabled:
		return []span{{"not enabled", t.warning}}
	case job.Approval == "needs_approval":
		return []span{{"needs approval", t.warning}}
	case job.Schedule == nil || job.NextAt == nil:
		return []span{{"—", t.faint}}
	case job.NextAt.Before(now.Add(-time.Minute)):
		return []span{{"overdue", t.warning}}
	}
	return []span{{untilText(*job.NextAt, now), t.text}}
}

func (m *Model) lastRunCells(run *app.Run, now time.Time) ([]span, []span) {
	t := m.theme
	if run == nil {
		return []span{{"never", t.faint}}, nil
	}
	icon, tone, label := m.runListStatus(*run)
	return []span{{icon + " " + label, tone}}, []span{{runWhen(*run, now), t.muted}}
}

func (m *Model) jobDetail(job app.Job) []string {
	t := m.theme
	about := []span{{" " + job.Name, t.strong}}
	if job.Description != "" {
		about = append(about, span{"  " + job.Description, t.text})
	} else {
		about = append(about, span{"  no description", t.faint})
	}
	var facts []string
	if job.Timeout != "" {
		facts = append(facts, "timeout "+compactDuration(job.Timeout))
	}
	if job.HasWebhook {
		facts = append(facts, "webhook")
	}
	if job.Source != "" {
		facts = append(facts, filepath.Base(job.Source))
	}
	command := []span{{" $ ", t.faint}, {commandText(job), t.muted}}
	return []string{
		fitSpans(about, m.width, style{}, false),
		justify(m.width, style{}, command, []span{{strings.Join(facts, " · ") + " ", t.faint}}),
	}
}

// Runs

var runColumns = []column{
	{title: "RUN", max: 8, right: true},
	{title: "STATUS", max: 14},
	{title: "TRIGGER", min: 7, max: 10, rank: 3},
	{title: "STARTED", min: 7, max: 14, rank: 1},
	{title: "DURATION", min: 8, max: 10, rank: 2},
	{title: "EXIT", min: 4, max: 12, rank: 4},
}

func (m *Model) runsBody(now time.Time) []string {
	t := m.theme
	layout := m.runsLayout()
	filter := statusFilters[m.statusFilter]
	labels := make([]string, len(statusFilters))
	for i, item := range statusFilters {
		labels[i] = item.label
	}
	title := append(m.breadcrumb("jobs", m.runsJob), span{"  " + filter.title, t.muted})
	rows := []string{justify(m.width, style{}, title, m.tabs(labels, m.statusFilter))}
	if layout.gap {
		rows = append(rows, "")
	}
	if len(m.runs) == 0 {
		empty := []span{{filter.empty, t.muted}}
		if m.statusFilter == 0 {
			empty = append(empty, span{"  r", t.key}, span{" runs it now", t.muted})
		}
		return append(rows, m.emptyRow(empty...))
	}
	cells := make([][][]span, len(m.runs))
	for i, run := range m.runs {
		cells[i] = m.runCells(run, now)
	}
	rows = append(rows, m.tableRows(newTable(runColumns, cells, m.width), cells, m.selectedRun, m.runOffset, layout.rows)...)
	if layout.detail {
		rows = append(rows, "")
		rows = append(rows, m.runDetail(m.runs[m.selectedRun], now)...)
	}
	return rows
}

func (m *Model) runCells(run app.Run, now time.Time) [][]span {
	t := m.theme
	icon, tone, label := m.runListStatus(run)
	started := sinceText(run.StartedAt, now)
	if run.Status == "queued" {
		started = "due " + app.RelativeTime(dueAt(run), now)
	}
	duration := span{durationText(run.DurationMS), t.muted}
	if run.Status == "running" {
		duration = span{elapsedText(run.StartedAt, now), t.running}
	}
	return [][]span{
		{{"#" + strconv.FormatInt(run.ID, 10), t.text}},
		{{icon + " " + label, tone}},
		{{run.Trigger, t.muted}},
		{{started, t.muted}},
		{duration},
		m.exitCell(run),
	}
}

func (m *Model) runListStatus(run app.Run) (string, style, string) {
	icon, tone := m.statusIcon(run.Status)
	label := statusLabel(run.Status)
	if run.Status == "running" && run.Orphaned {
		return "✗", m.theme.failure, "orphaned"
	}
	return icon, tone, label
}

func (m *Model) exitCell(run app.Run) []span {
	t := m.theme
	var spans []span
	if run.ExitCode != nil {
		tone := t.muted
		if *run.ExitCode != 0 && run.Status != "noop" {
			tone = t.failure
		}
		spans = append(spans, span{strconv.Itoa(*run.ExitCode), tone})
	}
	if run.Signal != nil {
		if len(spans) > 0 {
			spans = append(spans, span{" ", style{}})
		}
		spans = append(spans, span{*run.Signal, t.warning})
	}
	if len(spans) == 0 {
		return []span{{"—", t.faint}}
	}
	return spans
}

func (m *Model) runDetail(run app.Run, now time.Time) []string {
	t := m.theme
	facts := []span{{" ", style{}}}
	for i, fact := range m.runFacts(run, now) {
		if i > 0 {
			facts = append(facts, span{" · ", t.faint})
		}
		facts = append(facts, fact)
	}
	location := []span{{" log ", t.faint}, {m.homePath(logPath(run)), t.muted}}
	if run.Log.Available {
		location = append(location, span{" · " + byteSize(run.Log.Bytes), t.faint})
	}
	return []string{fitSpans(facts, m.width, style{}, false), fitSpans(location, m.width, style{}, false)}
}

// runFacts summarizes a run's outcome for the run detail and log header.
func (m *Model) runFacts(run app.Run, now time.Time) []span {
	t := m.theme
	var facts []span
	if run.Error != nil && *run.Error != "" {
		facts = append(facts, span{*run.Error, t.failure})
	}
	if run.ExitCode != nil && *run.ExitCode != 0 {
		tone := t.failure
		if run.Status == "noop" {
			tone = t.muted
		}
		facts = append(facts, span{"exit " + strconv.Itoa(*run.ExitCode), tone})
	}
	if run.Signal != nil {
		facts = append(facts, span{*run.Signal, t.warning})
	}
	facts = append(facts, span{run.Trigger, t.muted})
	switch {
	case run.Status == "queued":
		facts = append(facts, span{"due " + app.RelativeTime(dueAt(run), now), t.muted})
	case run.Status == "running":
		facts = append(facts, span{"running for " + elapsedText(run.StartedAt, now), t.running})
	case run.StartedAt != nil:
		facts = append(facts, span{"started " + sinceText(run.StartedAt, now), t.muted})
	}
	if run.Status != "running" && run.DurationMS != nil {
		facts = append(facts, span{"took " + durationText(run.DurationMS), t.muted})
	}
	if run.Hook != nil {
		icon, tone := m.hookIcon(run.Hook.Status)
		text := icon + " " + run.Hook.Event + " hook " + statusLabel(run.Hook.Status)
		if run.Hook.ExitCode != nil && *run.Hook.ExitCode != 0 {
			text += " (exit " + strconv.Itoa(*run.Hook.ExitCode) + ")"
		}
		facts = append(facts, span{text, tone})
	}
	return facts
}

// Help

type helpEntry struct{ keys, text string }

type helpSection struct {
	title   string
	entries []helpEntry
}

// helpColumns lists every shortcut; the status line only offers "? help".
// Two columns fit an 80x24 terminal.
var helpColumns = [2][]helpSection{{
	{"Navigate", []helpEntry{
		{"j k  ↓ ↑", "down / up"},
		{"g G", "first / last"},
		{"ctrl+d ctrl+u", "half page down / up"},
		{"ctrl+f ctrl+b", "page down / up"},
		{"enter →", "open"},
		{"esc h ←", "back"},
		{"?", "toggle this help"},
		{"q ctrl+c", "quit"},
	}},
	{"Jobs and runs", []helpEntry{
		{"l", "open the log"},
		{"r", "run the job now"},
		{"p", "pause or resume"},
		{"f tab", "filter by status"},
		{"c", "cancel (asks first)"},
	}},
}, {
	{"Logs", []helpEntry{
		{"/", "search (RE2)"},
		{"n N", "next / prev match"},
		{"s tab", "job → hook → all"},
		{"w", "wrap long lines"},
		{"t", "toggle timestamps"},
		{"g G", "start / end, follow"},
		{"space b", "page down / up"},
	}},
	{"Search prompt", []helpEntry{
		{"enter", "search"},
		{"esc", "cancel"},
		{"ctrl+u ctrl+w", "clear / delete word"},
	}},
}}

var legendStatuses = []string{"succeeded", "noop", "failed", "timed_out", "running", "queued", "cancelled", "skipped"}

const (
	helpKeyWidth    = 15
	helpColumnWidth = 34
)

// helpRows lays out the shortcuts and status legend, in two columns when the
// window fits them.
func (m *Model) helpRows() []string {
	t := m.theme
	width := helpColumnWidth
	twoColumns := m.width >= 2*helpColumnWidth+6
	if !twoColumns {
		width = max(1, m.width-3)
	}
	pair := func(left, right []span) string {
		return fitSpans(left, min(helpKeyWidth, width), style{}, false) + fitSpans(right, max(0, width-helpKeyWidth), style{}, false)
	}
	var columns [2][]string
	for i, sections := range helpColumns {
		for _, section := range sections {
			if len(columns[i]) > 0 {
				columns[i] = append(columns[i], "")
			}
			columns[i] = append(columns[i], fitSpans([]span{{section.title, t.strong}}, width, style{}, false))
			for _, entry := range section.entries {
				columns[i] = append(columns[i], pair([]span{{entry.keys, t.key}}, []span{{entry.text, t.text}}))
			}
		}
	}
	columns[1] = append(columns[1], "", fitSpans([]span{{"Status", t.strong}}, width, style{}, false))
	for i := 0; i < len(legendStatuses); i += 2 {
		var cells [2][]span
		for j, status := range legendStatuses[i : i+2] {
			icon, tone := m.statusIcon(status)
			cells[j] = []span{{icon + " " + statusLabel(status), tone}}
		}
		columns[1] = append(columns[1], pair(cells[0], cells[1]))
	}
	var rows []string
	if !twoColumns {
		for _, row := range append(append(columns[0], ""), columns[1]...) {
			rows = append(rows, "   "+row)
		}
		return rows
	}
	blank := strings.Repeat(" ", width)
	for i := range max(len(columns[0]), len(columns[1])) {
		left, right := blank, ""
		if i < len(columns[0]) && columns[0][i] != "" {
			left = columns[0][i]
		}
		if i < len(columns[1]) {
			right = columns[1][i]
		}
		rows = append(rows, "   "+left+"   "+right)
	}
	return rows
}

func (m *Model) helpBodyHeight() int {
	top, bottom := m.frame()
	return max(1, m.height-top-bottom-3)
}

func (m *Model) maxHelpScroll() int {
	return max(0, len(m.helpRows())-m.helpBodyHeight())
}

func (m *Model) helpBody() []string {
	rows := []string{
		fitSpans(m.breadcrumb("keyboard shortcuts"), m.width, style{}, false),
		fitSpans([]span{{" The TUI never adds, edits, enables, or approves jobs; use the CLI.", m.theme.muted}}, m.width, style{}, false),
		"",
	}
	content := m.helpRows()
	scroll := clamp(m.helpScroll, 0, m.maxHelpScroll())
	return append(rows, content[scroll:min(len(content), scroll+m.helpBodyHeight())]...)
}

// Footer

func (m *Model) statusRow() string {
	t := m.theme
	if m.searchPrompt {
		text, _ := m.promptText()
		return fitSpans([]span{{" /", t.key}, {text, t.text}}, m.width, style{}, false)
	}
	var left []span
	switch {
	case m.confirmCancelID != 0:
		question := fmt.Sprintf("Cancel run %d", m.confirmCancelID)
		if job := m.selectedActionJob(); job != "" {
			question += " of " + job
		}
		emphasis := t.warning
		emphasis.bold = true
		left = []span{{" ▲ " + question + "?", emphasis}}
	case m.message != "":
		left = m.messageSpans()
	case !m.showBanner() && len(m.bannerItems()) > 0:
		// Short windows have no banner row, so the first notice moves here.
		item := m.bannerItems()[0]
		left = []span{{" " + item.icon + " " + item.text, item.tone}}
		if item.fix != "" {
			left = append(left, span{"  " + item.fix, t.cmd})
		}
	}
	items := m.positionItems()
	if m.confirmCancelID != 0 {
		// The answer keys are short and on the right, so they stay visible
		// when a narrow window cuts the question short.
		items = [][]span{{{"y", t.key}, {"/", t.muted}, {"n", t.key}}}
	} else {
		help := []span{{"?", t.key}, {" help", t.muted}}
		if m.screen == helpScreen {
			help = []span{{"esc", t.key}, {" close", t.muted}}
		}
		// The help entry point yields when a message needs the room.
		if withHelp := append(items[:len(items):len(items)], help); spansWidth(left)+spansWidth(joinItems(withHelp))+4 <= m.width {
			items = withHelp
		}
	}
	right := joinItems(items)
	if len(right) > 0 {
		right = append(right, span{" ", style{}})
	}
	return justify(m.width, style{}, left, right)
}

func (m *Model) messageSpans() []span {
	t := m.theme
	icon, tone, text := "•", t.accent, t.text
	switch m.messageTone {
	case toneSuccess:
		icon, tone, text = "✓", t.success, t.success
	case toneWarning:
		icon, tone, text = "▲", t.warning, t.warning
	case toneError:
		icon, tone, text = "✗", t.failure, t.failure
	}
	head, tail, found := strings.Cut(m.message, " · ")
	spans := []span{{" " + icon + " ", tone}, {head, text}}
	if found {
		spans = append(spans, span{" · " + tail, t.muted})
	}
	return spans
}

// positionItems shows where the view is within a list, log, or help page.
func (m *Model) positionItems() [][]span {
	t := m.theme
	var items [][]span
	add := func(spans ...span) { items = append(items, spans) }
	switch m.screen {
	case jobsScreen:
		if total := len(m.dashboard.Jobs); total > m.jobsLayout().rows {
			add(span{fmt.Sprintf("%d/%d", m.selectedJob+1, total), t.faint})
		}
	case runsScreen:
		if total := len(m.runs); total > m.runsLayout().rows {
			add(span{fmt.Sprintf("%d/%d", m.selectedRun+1, total), t.faint})
		}
	case logScreen:
		if matches := len(m.logRows().matches); m.logSearch != "" && matches > 0 {
			add(span{fmt.Sprintf("match %d/%d", m.matchCursor+1, matches), t.accent})
		}
		if activeRun(m.logRun) && !m.logOrphaned && m.logSearch == "" {
			if m.autoScroll {
				add(span{m.spinner() + " following", t.running})
			} else {
				add(span{"G", t.key}, span{" to follow", t.muted})
			}
		}
		if position := scrollPosition(m.logScroll, m.logBodyHeight(), len(m.logRows().rows), m.autoScroll); position != "" {
			add(span{position, t.faint})
		}
	case helpScreen:
		if position := scrollPosition(m.helpScroll, m.helpBodyHeight(), len(m.helpRows()), false); position != "" {
			add(span{position, t.faint})
		}
	}
	return items
}

func joinItems(items [][]span) []span {
	var spans []span
	for i, item := range items {
		if i > 0 {
			spans = append(spans, span{"   ", style{}})
		}
		spans = append(spans, item...)
	}
	return spans
}

func scrollPosition(scroll, height, rows int, atEnd bool) string {
	switch {
	case rows <= height:
		return ""
	case atEnd || scroll >= rows-height:
		return "end"
	case scroll == 0:
		return "top"
	}
	return fmt.Sprintf("%d%%", (scroll+height)*100/rows)
}

// promptText returns the visible search input and the cursor column, keeping
// the cursor on screen for long patterns.
func (m *Model) promptText() (string, int) {
	room := max(2, m.width-3)
	before := truncateLeft(string(m.searchInput[:m.searchCursor]), room-1)
	after := displayText(string(m.searchInput[m.searchCursor:]))
	after = ansi.Truncate(after, max(0, room-ansi.StringWidth(before)), "…")
	return before + after, 2 + ansi.StringWidth(before)
}

// Formatting

func (m *Model) spinner() string {
	return spinnerFrames[m.spinFrame%len(spinnerFrames)]
}

func (m *Model) statusIcon(status string) (string, style) {
	t := m.theme
	switch status {
	case "succeeded":
		return "✓", t.success
	case "failed":
		return "✗", t.failure
	case "timed_out", "interrupted":
		return "✗", t.warning
	case "running":
		return m.spinner(), t.running
	case "queued":
		return "◌", t.queued
	case "cancelled":
		return "⊘", t.muted
	case "noop":
		return "–", t.muted
	}
	return "–", t.muted
}

func (m *Model) hookIcon(status string) (string, style) {
	t := m.theme
	switch status {
	case "ok":
		return "✓", t.success
	case "running":
		return m.spinner(), t.running
	case "failed", "timed_out":
		return "✗", t.failure
	}
	return "–", t.muted
}

func statusLabel(status string) string { return strings.ReplaceAll(status, "_", " ") }

func failedStatus(status string) bool {
	return status == "failed" || status == "timed_out" || status == "interrupted"
}

func dueAt(run app.Run) *string {
	if run.ScheduledAt != nil {
		return run.ScheduledAt
	}
	return run.DueAt
}

// runWhen is the short time context for a run's status: elapsed while
// running, due time while queued, and end time afterward.
func runWhen(run app.Run, now time.Time) string {
	switch run.Status {
	case "running":
		return elapsedText(run.StartedAt, now)
	case "queued":
		return app.RelativeTime(dueAt(run), now)
	}
	if run.FinishedAt != nil {
		return sinceText(run.FinishedAt, now)
	}
	return sinceText(run.StartedAt, now)
}

// sinceText is app.RelativeTime with seconds for the last minute, which the
// views refresh every second.
func sinceText(value *string, now time.Time) string {
	if value != nil {
		if at, err := time.Parse(time.RFC3339Nano, *value); err == nil {
			if elapsed := now.Sub(at); elapsed >= 0 && elapsed < time.Minute {
				return fmt.Sprintf("%ds ago", int(elapsed/time.Second))
			}
		}
	}
	return app.RelativeTime(value, now)
}

func untilText(at, now time.Time) string {
	wait := at.Sub(now)
	switch {
	case wait < time.Minute:
		return "now"
	case wait < time.Hour:
		return fmt.Sprintf("in %dm", int(wait/time.Minute))
	case wait < 24*time.Hour:
		if minutes := int(wait % time.Hour / time.Minute); minutes > 0 {
			return fmt.Sprintf("in %dh %dm", int(wait/time.Hour), minutes)
		}
		return fmt.Sprintf("in %dh", int(wait/time.Hour))
	case wait < 6*24*time.Hour:
		return at.Format("Mon 15:04")
	}
	return at.Format("Jan 2 15:04")
}

func elapsedText(start *string, now time.Time) string {
	if start == nil {
		return "—"
	}
	at, err := time.Parse(time.RFC3339Nano, *start)
	if err != nil {
		return "—"
	}
	elapsed := max(0, now.Sub(at)).Milliseconds()
	return durationText(&elapsed)
}

// durationText keeps sub-second precision that rounding to seconds would hide.
func durationText(ms *int64) string {
	if ms == nil {
		return "—"
	}
	value := time.Duration(*ms) * time.Millisecond
	switch {
	case value < time.Second:
		return fmt.Sprintf("%dms", *ms)
	case value < 10*time.Second:
		return fmt.Sprintf("%.1fs", value.Seconds())
	case value < time.Minute:
		return fmt.Sprintf("%ds", int(value/time.Second))
	case value < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(value/time.Minute), int(value%time.Minute/time.Second))
	}
	return fmt.Sprintf("%dh %02dm", int(value/time.Hour), int(value%time.Hour/time.Minute))
}

func compactDuration(text string) string {
	value, err := time.ParseDuration(text)
	if err != nil {
		return text
	}
	out := value.String()
	if strings.HasSuffix(out, "m0s") {
		out = strings.TrimSuffix(out, "0s")
	}
	if strings.HasSuffix(out, "h0m") {
		out = strings.TrimSuffix(out, "0m")
	}
	return out
}

func byteSize(size int64) string {
	switch {
	case size < 1<<10:
		return fmt.Sprintf("%d B", size)
	case size < 1<<20:
		return fmt.Sprintf("%.1f KiB", float64(size)/(1<<10))
	}
	return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
}

func logPath(run app.Run) string {
	if run.Log.Path == nil {
		return "unavailable"
	}
	return *run.Log.Path
}

// commandText summarizes what a job runs on one line.
func commandText(job app.Job) string {
	switch run := job.Run.(type) {
	case string:
		return firstLine(run)
	case []string:
		args := make([]string, len(run))
		for i, arg := range run {
			args[i] = shellWord(arg)
		}
		return strings.Join(args, " ")
	}
	switch {
	case job.PromptFile != nil:
		return job.Agent + ": " + *job.PromptFile
	case job.Prompt != nil:
		return job.Agent + ": " + firstLine(*job.Prompt)
	}
	return ""
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	line, rest, found := strings.Cut(text, "\n")
	if found && strings.TrimSpace(rest) != "" {
		return strings.TrimSpace(line) + " …"
	}
	return line
}

func shellWord(arg string) string {
	if arg == "" {
		return `""`
	}
	for _, r := range arg {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./_-", r)) {
			return strconv.Quote(arg)
		}
	}
	return arg
}

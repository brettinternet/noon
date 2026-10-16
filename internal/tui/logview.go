package tui

import (
	"fmt"
	"strings"
	"time"
)

type lineKind uint8

const (
	textLine lineKind = iota
	matchLine
	contextLine
	separatorLine
	partialLine
)

// logLine is one stored line prepared for display. Search results keep their
// grep-style line number prefix apart from the text so it can be styled.
type logLine struct {
	kind   lineKind
	prefix string
	text   string
	marks  [][]int // highlighted byte ranges in text
}

// logRow is the part of a line shown on one screen row.
type logRow struct {
	line, start, end int
}

// logRowCache holds the wrapped rows for the loaded log text, rebuilt only
// when the text, width, wrap mode, or search changes.
type logRowCache struct {
	built   bool
	version int
	width   int
	wrap    bool
	search  string
	partial bool
	lines   []logLine
	rows    []logRow
	matches []int // first row of each matching line
}

func (m *Model) logRows() *logRowCache {
	cache := &m.logRowCache
	width := max(1, m.width)
	if cache.built && cache.version == m.logVersion && cache.width == width && cache.wrap == m.logWrap && cache.search == m.logSearch && cache.partial == m.logPartial {
		return cache
	}
	*cache = logRowCache{built: true, version: m.logVersion, width: width, wrap: m.logWrap, search: m.logSearch, partial: m.logPartial}
	if m.logPartial && m.logText != "" {
		text := "earlier output is not loaded · g loads the start"
		if m.logSearch != "" {
			text = "earlier matches are not shown · a narrower pattern shows them"
		}
		cache.lines = append(cache.lines, logLine{kind: partialLine, text: text})
	}
	for _, raw := range splitLogLines(m.logText) {
		cache.lines = append(cache.lines, m.parseLogLine(raw))
	}
	for i, line := range cache.lines {
		if line.kind == matchLine {
			cache.matches = append(cache.matches, len(cache.rows))
		}
		breaks := []int{0}
		if m.logWrap && line.kind != partialLine {
			breaks = wrapText(line.text, max(1, width-len(line.prefix)))
		}
		for j, start := range breaks {
			end := len(line.text)
			if j+1 < len(breaks) {
				end = breaks[j+1]
			}
			cache.rows = append(cache.rows, logRow{line: i, start: start, end: end})
		}
	}
	return cache
}

func (m *Model) parseLogLine(raw string) logLine {
	if m.logSearch == "" {
		return logLine{text: logLineText(raw)}
	}
	if raw == "--" {
		return logLine{kind: separatorLine, text: "--"}
	}
	digits := 0
	for digits < len(raw) && raw[digits] >= '0' && raw[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits == len(raw) || raw[digits] != ':' && raw[digits] != '-' {
		return logLine{text: logLineText(raw)}
	}
	line := logLine{kind: contextLine, prefix: raw[:digits+1], text: logLineText(raw[digits+1:])}
	if raw[digits] == ':' {
		line.kind = matchLine
		if m.searchRE != nil {
			for _, mark := range m.searchRE.FindAllStringIndex(line.text, -1) {
				if mark[1] > mark[0] {
					line.marks = append(line.marks, mark)
				}
			}
		}
	}
	return line
}

// logLayout splits the log screen into run header, body, and notice rows.
func (m *Model) logLayout() (header, body, notices int) {
	top, bottom := m.frame()
	available := m.height - top - bottom
	header = 1
	if available >= 10 {
		header = 3
	}
	if m.logNotice != "" {
		notices++
	}
	if m.followNotice {
		notices++
	}
	if available-header-notices < 3 {
		notices = 0
	}
	return header, max(1, available-header-notices), notices
}

func (m *Model) logBodyHeight() int {
	_, body, _ := m.logLayout()
	return body
}

func (m *Model) logBody(now time.Time) []string {
	header, height, notices := m.logLayout()
	rows := m.logHeader(now)[:header]
	cache := m.logRows()
	maxScroll := max(0, len(cache.rows)-height)
	scroll := clamp(m.logScroll, 0, maxScroll)
	if m.autoScroll {
		scroll = maxScroll
	}
	if len(cache.rows) == 0 {
		rows = append(rows, fitSpans([]span{{" " + m.emptyLogText(), m.theme.faint}}, m.width, style{}, false))
	}
	current := -1
	if m.logSearch != "" && len(cache.matches) > 0 {
		current = cache.rows[cache.matches[clamp(m.matchCursor, 0, len(cache.matches)-1)]].line
	}
	for i := scroll; i < min(len(cache.rows), scroll+height); i++ {
		rows = append(rows, m.logRowText(cache, i, cache.rows[i].line == current))
	}
	for len(rows) < header+height {
		rows = append(rows, "")
	}
	if notices > 0 {
		rows = append(rows, m.logNoticeRows()...)
	}
	return rows
}

// logHeader shows the run path, status, facts, and log file above a rule.
func (m *Model) logHeader(now time.Time) []string {
	t := m.theme
	run := m.logRun
	icon, tone := m.statusIcon(run.Status)
	status := statusLabel(run.Status)
	if m.logOrphaned {
		icon, tone, status = "✗", t.failure, "orphaned"
	}
	title := append(m.breadcrumb("jobs", run.Job, fmt.Sprintf("run %d", run.ID)), span{"   " + icon + " " + status, tone})
	var sections []span
	if run.Log.HookOffset != nil {
		active := 0
		for i, section := range logSections {
			if section == m.logSection {
				active = i
			}
		}
		sections = m.tabs(logSections, active)
	}
	facts := []span{{" ", style{}}}
	for i, fact := range m.runFacts(run, now) {
		if i > 0 {
			facts = append(facts, span{" · ", t.faint})
		}
		facts = append(facts, fact)
	}
	// The log path ends in the run ID, so trim it from the left when space is short.
	path := m.homePath(logPath(run))
	if run.Log.Available {
		path += " · " + byteSize(m.logSize)
	}
	details := facts
	if room := m.width - spansWidth(facts) - 4; room >= 16 {
		details = append(details, span{"   " + truncateLeft(path, room), t.faint})
	}
	return []string{
		justify(m.width, style{}, title, sections),
		fitSpans(details, m.width, style{}, false),
		t.rule.render(strings.Repeat("─", max(0, m.width))),
	}
}

func (m *Model) logNoticeRows() []string {
	t := m.theme
	var rows []string
	if m.logNotice != "" {
		icon, tone := "◌", t.queued
		switch m.logNoticeTone {
		case toneError:
			icon, tone = "✗", t.failure
		case toneWarning:
			icon, tone = "▲", t.warning
		}
		head, tail, found := strings.Cut(m.logNotice, " · ")
		spans := []span{{" " + icon + " " + head, tone}}
		if found {
			spans = append(spans, span{"  " + tail, t.cmd})
		}
		rows = append(rows, fitSpans(spans, m.width, style{}, false))
	}
	if m.followNotice {
		rows = append(rows, fitSpans([]span{{" ▲ Following stops at 1 MiB; output past 1 MiB is shown when the run ends.", t.warning}}, m.width, style{}, false))
	}
	return rows
}

func (m *Model) emptyLogText() string {
	switch {
	case m.logSearch != "":
		return "No lines match /" + m.logSearch + "/"
	case m.logRun.Status == "queued" || activeRun(m.logRun) && !m.logOrphaned:
		return "Waiting for output…"
	case m.logSection == "hook":
		return "No hook output"
	case m.logSection == "job" && m.logRun.Log.HookOffset != nil:
		return "No job output · s shows the hook's output"
	}
	return "No output"
}

// logRowText renders one screen row. Log text is never padded, so rows copy
// cleanly; the current search match is the exception, filled to the edge.
func (m *Model) logRowText(cache *logRowCache, index int, current bool) string {
	t := m.theme
	row := cache.rows[index]
	line := cache.lines[row.line]
	switch line.kind {
	case partialLine:
		text, _ := clipSpans([]span{{"┄ " + line.text + " ┄", t.faint}}, m.width, style{})
		return text
	case separatorLine:
		return t.faint.render("--")
	}
	var spans []span
	if line.prefix != "" {
		prefix, tone := line.prefix, t.faint
		if line.kind == matchLine {
			tone = t.accent
		}
		if row.start > 0 {
			prefix = strings.Repeat(" ", len(prefix))
		}
		spans = append(spans, span{prefix, tone})
	}
	spans = append(spans, markedSpans(line.text[row.start:row.end], line.marks, row.start, t.match)...)
	if current {
		return fitSpans(spans, m.width, t.current, false)
	}
	text, _ := clipSpans(spans, m.width, style{})
	return text
}

// markedSpans splits text into plain and highlighted spans. Marks are byte
// ranges in the whole line; offset is where text starts within that line.
func markedSpans(text string, marks [][]int, offset int, highlight style) []span {
	var spans []span
	position := 0
	for _, mark := range marks {
		start, end := max(mark[0]-offset, position), min(mark[1]-offset, len(text))
		if end <= start {
			continue
		}
		if start > position {
			spans = append(spans, span{text[position:start], style{}})
		}
		spans = append(spans, span{text[start:end], highlight})
		position = end
	}
	if position < len(text) || len(spans) == 0 {
		spans = append(spans, span{text[position:], style{}})
	}
	return spans
}

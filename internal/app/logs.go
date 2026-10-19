package app

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/noon/internal/store"
)

// LogQuery selects a bounded view of one retained run log. Nil values mean the
// corresponding option was not supplied.
type LogQuery struct {
	Section              string
	HookOffset           *int64
	After                *int64
	Tail                 *int
	Match                *string
	Context              *int
	LimitBytes           *int
	RepairUTF8           bool
	Timestamps           bool
	IncludeActivePartial bool
}

// LogSelection is a snapshot of the bytes selected from a run log.
type LogSelection struct {
	Bytes                 []byte
	Next                  int64
	Size                  int64
	Truncated             bool
	MatchedLines          *int
	LineTimes             []LineTime
	TimestampContinuation bool
}

// LineTime associates one selected raw log line with the receipt time of its first byte.
type LineTime struct {
	Offset int64   `json:"offset"`
	Time   *string `json:"time"`
}

const (
	LogTimestampLayout = "2006-01-02T15:04:05.000-07:00"
	LogTimestampWidth  = 29
)

// LogTimestampLabel formats an optional UTC log time for local human display.
func LogTimestampLabel(value *string) string {
	if value == nil {
		return strings.Repeat("-", LogTimestampWidth)
	}
	parsed, err := time.Parse(store.TimestampLayout, *value)
	if err != nil {
		return strings.Repeat("-", LogTimestampWidth)
	}
	return parsed.Local().Format(LogTimestampLayout)
}

type selectedLogLine struct {
	data      []byte
	source    []byte
	start     int64
	end       int64
	lineStart int64
	lineTime  *string
	number    int64
	matched   bool
	prefixLen int
	separator bool
}

// ValidateLogQuery validates query fields that do not depend on the file size.
func ValidateLogQuery(query LogQuery) error {
	if query.Section != "" && query.Section != "all" && query.Section != "job" && query.Section != "hook" {
		return UsageError("section must be all, job, or hook", "Use --section all, --section job, or --section hook")
	}
	if query.After != nil && *query.After < 0 {
		return UsageError("after must not be negative", "")
	}
	if query.Tail != nil && (*query.Tail < 1 || *query.Tail > 10000) {
		return UsageError("tail must be between 1 and 10000", "")
	}
	if query.Context != nil && (*query.Context < 0 || *query.Context > 50) {
		return UsageError("context must be between 0 and 50", "")
	}
	if query.Context != nil && query.Match == nil {
		return UsageError("context requires match", "")
	}
	if query.LimitBytes != nil && *query.LimitBytes < 0 {
		return UsageError("limit-bytes must not be negative", "")
	}
	if query.Match != nil {
		if _, err := regexp.Compile(*query.Match); err != nil {
			return UsageError(fmt.Sprintf("invalid match regular expression: %v", err), "Use a valid Go RE2 regular expression")
		}
	}
	return nil
}

// SelectLog applies a query to one immutable byte snapshot without I/O.
func SelectLog(data []byte, active bool, query LogQuery) (LogSelection, error) {
	return selectLog(data, nil, active, query)
}

// SelectLogWithTiming applies a query and attaches timing captured for raw offsets.
func SelectLogWithTiming(data []byte, timing []store.TimingRecord, active bool, query LogQuery) (LogSelection, error) {
	return selectLog(data, timing, active, query)
}

func selectLog(data []byte, timing []store.TimingRecord, active bool, query LogQuery) (LogSelection, error) {
	if err := ValidateLogQuery(query); err != nil {
		return LogSelection{}, err
	}
	size := int64(len(data))
	after := int64(0)
	if query.After != nil {
		after = *query.After
		if after < 0 {
			return LogSelection{}, UsageError("after must not be negative", "")
		}
		if after > size {
			return LogSelection{}, UsageError("after must not exceed log size", "")
		}
	}
	rangeStart, rangeEnd := int64(0), size
	hookOffset := int64(0)
	hasHookOffset := query.HookOffset != nil
	if hasHookOffset {
		hookOffset = max(0, min(*query.HookOffset, size))
	}
	section := query.Section
	if section == "" {
		section = "all"
	}
	switch section {
	case "job":
		if hasHookOffset {
			rangeEnd = hookOffset
		}
	case "hook":
		if hasHookOffset {
			rangeStart = hookOffset
		} else {
			rangeEnd = 0
		}
	}
	start := max(after, rangeStart)
	end := min(size, rangeEnd)
	if end < start {
		end = start
	}
	if active && !query.IncludeActivePartial && end > start && data[end-1] != '\n' {
		completeEnd := int64(bytes.LastIndexByte(data[:end], '\n') + 1)
		if completeEnd < start {
			completeEnd = start
		}
		end = completeEnd
	}
	selection := LogSelection{Size: size, Next: end}
	var pattern *regexp.Regexp
	if query.Match != nil {
		count := 0
		selection.MatchedLines = &count
		pattern, _ = regexp.Compile(*query.Match)
	}

	lines := make([]selectedLogLine, 0)
	lineNumber := int64(bytes.Count(data[:start], []byte{'\n'})) + 1
	for lineStart := start; lineStart < end; lineNumber++ {
		relativeEnd := bytes.IndexByte(data[lineStart:end], '\n')
		lineEnd := end
		if relativeEnd >= 0 {
			lineEnd = lineStart + int64(relativeEnd) + 1
		}
		contentEnd := lineEnd
		if contentEnd > lineStart && data[contentEnd-1] == '\n' {
			contentEnd--
		}
		content := data[lineStart:contentEnd]
		rawLineStart := logLineStart(data, lineStart)
		var capturedTime *string
		if query.Timestamps {
			if receipt, ok := store.TimingAt(timing, rawLineStart); ok {
				formatted := time.UnixMilli(receipt).UTC().Format(store.TimestampLayout)
				capturedTime = &formatted
			}
		}
		if query.Match == nil {
			lineData := data[lineStart:lineEnd]
			line := selectedLogLine{start: lineStart, end: lineEnd, lineStart: rawLineStart, lineTime: capturedTime, number: lineNumber}
			if query.RepairUTF8 {
				line.data = []byte(ValidLogText(lineData))
				line.source = lineData
			} else {
				line.data = append([]byte(nil), lineData...)
			}
			lines = append(lines, line)
		} else {
			matched := pattern.Match(content)
			if matched {
				*selection.MatchedLines = *selection.MatchedLines + 1
			}
			line := selectedLogLine{
				data: append([]byte(nil), content...), start: lineStart, end: lineEnd, lineStart: rawLineStart,
				lineTime: capturedTime, number: lineNumber, matched: matched,
			}
			if query.RepairUTF8 {
				line.source = data[lineStart:lineEnd]
			}
			lines = append(lines, line)
		}
		lineStart = lineEnd
	}

	if query.Match != nil {
		contextLines := 0
		if query.Context != nil {
			contextLines = *query.Context
		}
		type window struct{ first, last int }
		windows := make([]window, 0)
		for i := range lines {
			if !lines[i].matched {
				continue
			}
			current := window{first: max(0, i-contextLines), last: min(len(lines)-1, i+contextLines)}
			if len(windows) > 0 && current.first <= windows[len(windows)-1].last+1 {
				windows[len(windows)-1].last = max(windows[len(windows)-1].last, current.last)
			} else {
				windows = append(windows, current)
			}
		}
		selected := make([]selectedLogLine, 0)
		for i, current := range windows {
			if i > 0 {
				selected = append(selected, selectedLogLine{data: []byte("--\n"), separator: true})
			}
			for j := current.first; j <= current.last; j++ {
				marker := byte('-')
				if lines[j].matched {
					marker = ':'
				}
				prefix := []byte(fmt.Sprintf("%d%c", lines[j].number, marker))
				content := lines[j].data
				if query.RepairUTF8 {
					content = []byte(ValidLogText(content))
				}
				entry := append(prefix, content...)
				entry = append(entry, '\n')
				selected = append(selected, selectedLogLine{
					data: entry, source: lines[j].source, start: lines[j].start, end: lines[j].end,
					lineStart: lines[j].lineStart, lineTime: lines[j].lineTime,
					number: lines[j].number, prefixLen: len(prefix),
				})
			}
		}
		lines = selected
	}

	if query.Tail != nil && len(lines) > *query.Tail {
		lines = lines[len(lines)-*query.Tail:]
		for len(lines) > 0 && lines[0].separator {
			lines = lines[1:]
		}
	}

	if query.LimitBytes == nil {
		selection.Bytes = concatenateLogLines(lines)
		if query.Timestamps {
			selection.LineTimes = lineTimesFor(lines)
			selection.TimestampContinuation = isLogContinuation(data, after, query.After != nil, lines)
		}
		return selection, nil
	}
	limit := *query.LimitBytes
	if query.After == nil {
		var included []selectedLogLine
		selection.Bytes, selection.Truncated, included = tailLimit(lines, limit)
		if query.Timestamps {
			selection.LineTimes = lineTimesFor(included)
			selection.TimestampContinuation = isLogContinuation(data, after, query.After != nil, included)
		}
		return selection, nil
	}
	selected, truncated, next, included, err := forwardLimit(lines, limit, start, end)
	if err != nil {
		return LogSelection{}, err
	}
	selection.Bytes, selection.Truncated, selection.Next = selected, truncated, next
	if query.Timestamps {
		selection.LineTimes = lineTimesFor(included)
		selection.TimestampContinuation = isLogContinuation(data, after, query.After != nil, included)
	}
	return selection, nil
}

// ReadRunLogSelection reads one run-log snapshot and applies the shared pure selector.
func ReadRunLogSelection(stateDir string, id int64, query LogQuery) (Run, LogSelection, error) {
	if err := ValidateLogQuery(query); err != nil {
		return Run{}, LogSelection{}, err
	}
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		return Run{}, LogSelection{}, err
	}
	defer state.Close()
	stored, err := state.RunByID(id)
	if err != nil {
		return Run{}, LogSelection{}, err
	}
	run := RunFromStore(state, stored)
	var data []byte
	var timing []store.TimingRecord
	if query.Timestamps {
		data, timing, err = state.ReadRunLogWithTiming(stored)
	} else {
		data, err = state.ReadRunLog(stored)
	}
	if IsMissingLog(err) {
		run.Log.Available = false
		run.Log.Path = nil
		data = nil
	} else if err != nil {
		return Run{}, LogSelection{}, err
	}
	if stored.HookLogOffset.Valid {
		offset := stored.HookLogOffset.Int64
		query.HookOffset = &offset
	}
	if query.Section == "job" && stored.Status != "running" && stored.HookStatus.Valid && stored.HookStatus.String == "running" {
		query.IncludeActivePartial = true
		if !stored.HookLogOffset.Valid {
			// The hook append precedes its database commit. Until the boundary
			// is published, only the recorded job bytes belong to this section.
			offset := stored.LogBytes
			query.HookOffset = &offset
		}
	}
	selection, err := SelectLogWithTiming(data, timing, activeStoredRun(stored), query)
	if err != nil {
		return Run{}, LogSelection{}, err
	}
	return run, selection, nil
}

// FollowLogStart returns the starting file offset for a follow snapshot.
func FollowLogStart(data []byte, after *int64, tail *int) (int64, error) {
	query := LogQuery{After: after, Tail: tail}
	if err := ValidateLogQuery(query); err != nil {
		return 0, err
	}
	start := int64(0)
	if after != nil {
		start = *after
		if start < 0 {
			return 0, UsageError("after must not be negative", "")
		}
		if start > int64(len(data)) {
			return 0, UsageError("after must not exceed log size", "")
		}
	}
	if tail == nil {
		return start, nil
	}
	window := data[start:]
	lineStarts := []int{0}
	for i, b := range window {
		if b == '\n' && i+1 < len(window) {
			lineStarts = append(lineStarts, i+1)
		}
	}
	index := max(0, len(lineStarts)-*tail)
	return start + int64(lineStarts[index]), nil
}

func logLineStart(data []byte, offset int64) int64 {
	if offset <= 0 {
		return 0
	}
	if newline := bytes.LastIndexByte(data[:offset], '\n'); newline >= 0 {
		return int64(newline + 1)
	}
	return 0
}

func isLogContinuation(data []byte, after int64, hasAfter bool, lines []selectedLogLine) bool {
	if !hasAfter || after <= 0 || data[after-1] == '\n' {
		return false
	}
	for _, line := range lines {
		if !line.separator {
			return line.start == after
		}
	}
	return false
}

func lineTimesFor(lines []selectedLogLine) []LineTime {
	var times []LineTime
	for _, line := range lines {
		if line.separator {
			continue
		}
		times = append(times, LineTime{Offset: line.lineStart, Time: line.lineTime})
	}
	return times
}

type timestampStream struct{ atLineStart bool }

func (stream *timestampStream) write(out io.Writer, data []byte, offset int64, timing []store.TimingRecord, unavailable bool) error {
	for index := 0; index < len(data); {
		if stream.atLineStart {
			var receipt *string
			if !unavailable {
				if millis, ok := store.TimingAt(timing, offset+int64(index)); ok {
					formatted := time.UnixMilli(millis).UTC().Format(store.TimestampLayout)
					receipt = &formatted
				}
			}
			prefix := LogTimestampLabel(receipt) + " "
			if n, err := io.WriteString(out, prefix); err != nil {
				return err
			} else if n != len(prefix) {
				return io.ErrShortWrite
			}
			stream.atLineStart = false
		}
		end := len(data)
		if newline := bytes.IndexByte(data[index:], '\n'); newline >= 0 {
			end = index + newline + 1
		}
		if n, err := out.Write(data[index:end]); err != nil {
			return err
		} else if n != end-index {
			return io.ErrShortWrite
		}
		stream.atLineStart = data[end-1] == '\n'
		index = end
	}
	return nil
}

func concatenateLogLines(lines []selectedLogLine) []byte {
	var output []byte
	for _, line := range lines {
		output = append(output, line.data...)
	}
	return output
}

func tailLimit(lines []selectedLogLine, limit int) ([]byte, bool, []selectedLogLine) {
	remaining := limit
	start := len(lines)
	var partial []byte
	truncated := false
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line.separator && remaining < len(line.data) {
			truncated = true
			break
		}
		if len(line.data) <= remaining {
			remaining -= len(line.data)
			start = i
			continue
		}
		truncated = true
		if start == len(lines) && !line.separator {
			partial = utf8Tail(line.data, remaining)
			start = i
		}
		break
	}
	if start > 0 {
		truncated = true
	}
	for start < len(lines) && lines[start].separator {
		start++
	}
	chosen := append([]selectedLogLine(nil), lines[start:]...)
	var output []byte
	if partial == nil {
		output = concatenateLogLines(chosen)
	} else {
		chosen[0].data = partial
		if len(partial) > 0 {
			output = append([]byte(nil), partial...)
			output = append(output, concatenateLogLines(chosen[1:])...)
		} else {
			chosen = chosen[1:]
			output = concatenateLogLines(chosen)
		}
	}
	return output, truncated, chosen
}

func forwardLimit(lines []selectedLogLine, limit int, after, end int64) ([]byte, bool, int64, []selectedLogLine, error) {
	var output []byte
	var chosen []selectedLogLine
	remaining := limit
	next := after
	for _, line := range lines {
		if len(line.data) <= remaining {
			output = append(output, line.data...)
			remaining -= len(line.data)
			chosen = append(chosen, line)
			if !line.separator {
				next = line.end
			}
			continue
		}
		if line.separator || len(output) != 0 || len(line.data) <= limit {
			return output, true, next, chosen, nil
		}
		partial := utf8Head(line.data, limit)
		consumed := max(0, len(partial)-line.prefixLen)
		if line.source != nil {
			consumed = sourceBytesForText(line.source, consumed)
		}
		if consumed == 0 {
			return nil, false, after, nil, UsageError("limit-bytes is too small to include one source rune after formatting", "Increase the byte limit to fit a source rune and any match prefix.")
		}
		output = append(output, partial...)
		line.data = partial
		chosen = append(chosen, line)
		next = line.start + int64(min(consumed, int(line.end-line.start)))
		return output, true, next, chosen, nil
	}
	return output, false, end, chosen, nil
}

func utf8Head(data []byte, limit int) []byte {
	limit = min(limit, len(data))
	end := 0
	for end < limit {
		_, size := utf8.DecodeRune(data[end:])
		if size == 0 || end+size > limit {
			break
		}
		end += size
	}
	return data[:end]
}

func sourceBytesForText(source []byte, outputBytes int) int {
	consumed, emitted := 0, 0
	for consumed < len(source) && emitted < outputBytes {
		r, size := utf8.DecodeRune(source[consumed:])
		if r == utf8.RuneError && size == 1 {
			end := consumed + size
			for end < len(source) {
				next, nextSize := utf8.DecodeRune(source[end:])
				if next != utf8.RuneError || nextSize != 1 {
					break
				}
				end += nextSize
			}
			if emitted+len("\uFFFD") > outputBytes {
				break
			}
			emitted += len("\uFFFD")
			consumed = end
			continue
		}
		if emitted+size > outputBytes {
			break
		}
		emitted += size
		consumed += size
	}
	return consumed
}

func utf8Tail(data []byte, limit int) []byte {
	if limit >= len(data) {
		return data
	}
	start := len(data) - limit
	for start < len(data) && !utf8.RuneStart(data[start]) {
		start++
	}
	return data[start:]
}

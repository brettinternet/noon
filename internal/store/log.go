package store

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// RunLog writes the first MiB directly and buffers only the last MiB of later output.
type RunLog struct {
	mu               sync.Mutex
	file             *os.File
	timing           *os.File
	path             string
	first            int64
	tail             []byte
	tailBytes        int64
	tailTiming       []TimingRecord
	lastByte         byte
	hasLastByte      bool
	headLastByte     byte
	hasHeadLastByte  bool
	persisted        int64
	closed           bool
	overflowNotified bool
	onOverflow       func() error
	now              func() time.Time
	writeErr         error
}

func (l *RunLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, os.ErrClosed
	}
	if l.writeErr != nil {
		return 0, l.writeErr
	}
	written := len(p)
	at := time.Now()
	if l.now != nil {
		at = l.now()
	}
	if remaining := int64(maxRunLogBytes) - l.first; remaining > 0 {
		n := min(len(p), int(remaining))
		if n > 0 {
			chunk := p[:n]
			records := TimingRecordsForChunk(l.first, chunk, l.lastByte, l.hasLastByte, at.UnixMilli())
			l.appendTiming(records)
			count, err := l.file.Write(chunk)
			l.first += int64(count)
			if count > 0 {
				l.lastByte, l.hasLastByte = chunk[count-1], true
				l.headLastByte, l.hasHeadLastByte = chunk[count-1], true
			}
			if err != nil {
				l.writeErr = err
				return count, err
			}
			if count != n {
				l.writeErr = fmt.Errorf("short run log write")
				return count, l.writeErr
			}
			p = p[n:]
		}
	}
	if len(p) > 0 {
		if !l.overflowNotified && l.onOverflow != nil {
			if err := l.onOverflow(); err != nil {
				l.writeErr = err
				return written - len(p), err
			}
			l.overflowNotified = true
		}
		tailStart := l.first + l.tailBytes
		l.tailBytes += int64(len(p))
		if len(p) >= maxRunLogBytes {
			l.tail = append(l.tail[:0], p[len(p)-maxRunLogBytes:]...)
		} else {
			drop := max(0, len(l.tail)+len(p)-maxRunLogBytes)
			if drop > 0 {
				copy(l.tail, l.tail[drop:])
				l.tail = l.tail[:len(l.tail)-drop]
			}
			l.tail = append(l.tail, p...)
		}
		// Tail records are contiguous; merging same-millisecond writes bounds them
		// by distinct receipt times rather than by write count.
		if last := len(l.tailTiming) - 1; last >= 0 && l.tailTiming[last].End == tailStart && l.tailTiming[last].UnixMilli == at.UnixMilli() {
			l.tailTiming[last].End += int64(len(p))
		} else {
			l.tailTiming = append(l.tailTiming, TimingRecord{Start: tailStart, End: tailStart + int64(len(p)), UnixMilli: at.UnixMilli()})
		}
		l.clipTailTiming(l.first + l.tailBytes - int64(len(l.tail)))
		l.lastByte, l.hasLastByte = p[len(p)-1], true
	}
	return written, nil
}

// Close appends the omission marker and tail, then returns persisted log metadata.
func (l *RunLog) Close() (bytes int64, truncated bool, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.persisted, l.tailBytes > maxRunLogBytes, l.writeErr
	}
	l.closed = true
	if l.writeErr == nil {
		omitted := l.tailBytes - int64(len(l.tail))
		marker := ""
		if omitted > 0 {
			marker = fmt.Sprintf("\n[noon: %d bytes omitted]\n", omitted)
		}
		l.appendTiming(l.finalTailTiming(int64(len(marker)), omitted > 0))
		if marker != "" {
			if _, err = l.file.WriteString(marker); err != nil {
				l.writeErr = err
			}
		}
		if l.writeErr == nil && len(l.tail) > 0 {
			if _, err = l.file.Write(l.tail); err != nil {
				l.writeErr = err
			}
		}
	}
	if l.writeErr == nil {
		l.writeErr = l.file.Sync()
	}
	if l.timing != nil {
		_ = l.timing.Sync()
		l.disableTiming()
	}
	if closeErr := l.file.Close(); l.writeErr == nil {
		l.writeErr = closeErr
	}
	info, statErr := os.Stat(l.file.Name())
	if statErr != nil && l.writeErr == nil {
		l.writeErr = statErr
	}
	if info != nil {
		bytes = info.Size()
	}
	l.persisted = bytes
	truncated = l.tailBytes > maxRunLogBytes
	return bytes, truncated, l.writeErr
}

func (l *RunLog) appendTiming(records []TimingRecord) {
	if l.timing == nil || len(records) == 0 {
		return
	}
	if err := writeTimingRecords(l.timing, records); err != nil {
		l.disableTiming()
	}
}

// disableTiming stops further capture but keeps records already written, so
// earlier lines stay timed and later lines become unavailable. Readers ignore
// a torn final record.
func (l *RunLog) disableTiming() {
	if l.timing == nil {
		return
	}
	_ = l.timing.Close()
	l.timing = nil
}

func (l *RunLog) clipTailTiming(start int64) {
	first := 0
	for first < len(l.tailTiming) && l.tailTiming[first].End <= start {
		first++
	}
	// Reslicing keeps dropping amortized O(1); append reallocates only live records.
	l.tailTiming = l.tailTiming[first:]
	if len(l.tailTiming) > 0 && l.tailTiming[0].Start < start {
		l.tailTiming[0].Start = start
	}
}

func (l *RunLog) finalTailTiming(markerBytes int64, omitted bool) []TimingRecord {
	if len(l.tail) == 0 || len(l.tailTiming) == 0 {
		return nil
	}
	tailStart := l.first + l.tailBytes - int64(len(l.tail))
	outputStart := l.first + markerBytes
	var records []TimingRecord
	for _, chunk := range l.tailTiming {
		start := max(chunk.Start, tailStart)
		end := min(chunk.End, tailStart+int64(len(l.tail)))
		if start >= end {
			continue
		}
		first := int64(-1)
		for offset := start; offset < end; offset++ {
			index := int(offset - tailStart)
			lineStart := offset > tailStart && l.tail[index-1] == '\n'
			if offset == tailStart {
				lineStart = omitted || l.hasHeadLastByte && l.headLastByte == '\n'
			}
			if lineStart {
				first = offset
				break
			}
		}
		if first >= 0 {
			records = append(records, TimingRecord{
				Start: outputStart + first - tailStart, End: outputStart + end - tailStart,
				UnixMilli: chunk.UnixMilli,
			})
		}
	}
	return coalesceTimingRecords(records)
}

func coalesceTimingRecords(records []TimingRecord) []TimingRecord {
	if len(records) < 2 {
		return records
	}
	out := records[:1]
	for _, record := range records[1:] {
		last := &out[len(out)-1]
		if last.End == record.Start && last.UnixMilli == record.UnixMilli {
			last.End = record.End
		} else {
			out = append(out, record)
		}
	}
	return out
}

func (l *RunLog) Path() string { return l.path }

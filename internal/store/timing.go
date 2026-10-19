package store

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const timingRecordSize = 3 * 8

// TimingRecord covers raw log offsets [Start, End) with the receipt time of one
// output write, stored as Unix milliseconds.
type TimingRecord struct {
	Start, End int64
	UnixMilli  int64
}

func timingPath(logPath string) string { return logPath + ".timing" }

func writeTimingRecords(file *os.File, records []TimingRecord) error {
	if file == nil {
		return nil
	}
	var data [timingRecordSize]byte
	for _, record := range records {
		binary.LittleEndian.PutUint64(data[0:8], uint64(record.Start))
		binary.LittleEndian.PutUint64(data[8:16], uint64(record.End))
		binary.LittleEndian.PutUint64(data[16:24], uint64(record.UnixMilli))
		n, err := file.Write(data[:])
		if err != nil {
			return err
		}
		if n != len(data) {
			return io.ErrShortWrite
		}
	}
	return nil
}

func appendTimingRecords(path string, records []TimingRecord) error {
	if len(records) == 0 {
		return nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size()%timingRecordSize != 0 {
		return io.ErrUnexpectedEOF
	}
	if err := writeTimingRecords(file, records); err != nil {
		return err
	}
	return nil
}

func readTimingRecords(path string) []TimingRecord {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	records := make([]TimingRecord, 0, len(data)/timingRecordSize)
	var lastEnd int64 = -1
	for offset := 0; offset+timingRecordSize <= len(data); offset += timingRecordSize {
		record := TimingRecord{
			Start:     int64(binary.LittleEndian.Uint64(data[offset : offset+8])),
			End:       int64(binary.LittleEndian.Uint64(data[offset+8 : offset+16])),
			UnixMilli: int64(binary.LittleEndian.Uint64(data[offset+16 : offset+24])),
		}
		if record.Start < 0 || record.End <= record.Start || record.Start < lastEnd {
			continue
		}
		records = append(records, record)
		lastEnd = record.End
	}
	return records
}

// TimingAt returns the captured time covering a raw byte offset, if present.
func TimingAt(records []TimingRecord, offset int64) (int64, bool) {
	index := sort.Search(len(records), func(i int) bool { return records[i].Start > offset }) - 1
	if index < 0 || offset >= records[index].End {
		return 0, false
	}
	return records[index].UnixMilli, true
}

// TimingRecordsForChunk returns the record for a received chunk's first line start, if any.
func TimingRecordsForChunk(start int64, data []byte, previousByte byte, hasPrevious bool, unixMilli int64) []TimingRecord {
	if len(data) == 0 {
		return nil
	}
	first := int64(-1)
	if !hasPrevious && start == 0 || hasPrevious && previousByte == '\n' {
		first = start
	}
	if first < 0 {
		for i := 1; i < len(data); i++ {
			if data[i-1] == '\n' {
				first = start + int64(i)
				break
			}
		}
	}
	if first < 0 {
		return nil
	}
	return []TimingRecord{{Start: first, End: start + int64(len(data)), UnixMilli: unixMilli}}
}

// ReadRunLogWithTiming snapshots raw bytes first, then reads the optional sidecar.
// Sidecar errors never make an otherwise readable log unavailable.
func (s *Store) ReadRunLogWithTiming(run Run) ([]byte, []TimingRecord, error) {
	data, err := s.ReadRunLog(run)
	if err != nil {
		return nil, nil, err
	}
	relative := run.LogPath.String
	if relative == "" || relative == "." {
		return data, nil, nil
	}
	return data, readTimingRecords(timingPath(filepath.Join(s.dir, relative))), nil
}

// newTimingFile is called only after the log was created exclusively, so any
// existing sidecar is stale (for example from a reused run ID) and must not be
// read as timing for the new log.
func newTimingFile(path string) *os.File {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil
	}
	return file
}

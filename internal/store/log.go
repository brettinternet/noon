package store

import (
	"fmt"
	"os"
	"sync"
)

// RunLog writes the first MiB directly and buffers only the last MiB of later output.
type RunLog struct {
	mu               sync.Mutex
	file             *os.File
	path             string
	first            int64
	tail             []byte
	tailBytes        int64
	persisted        int64
	closed           bool
	overflowNotified bool
	onOverflow       func() error
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
	if remaining := int64(maxRunLogBytes) - l.first; remaining > 0 {
		n := min(len(p), int(remaining))
		if n > 0 {
			count, err := l.file.Write(p[:n])
			l.first += int64(count)
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
		if omitted > 0 {
			if _, err = fmt.Fprintf(l.file, "\n[bump: %d bytes omitted]\n", omitted); err != nil {
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

func (l *RunLog) Path() string { return l.path }

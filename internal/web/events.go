package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/brettinternet/noon/internal/app"
)

const (
	heartbeatInterval = 15 * time.Second
	streamWriteLimit  = 30 * time.Second
)

// events streams state changes and, with ?run=ID&after=OFFSET, that run's new
// log lines. Each event is small; a tab refetches what it shows.
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	if !h.signedIn(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var runID int64
	var after *int64
	if value := r.URL.Query().Get("run"); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "run must be a run ID", http.StatusBadRequest)
			return
		}
		if _, err := app.EnabledRunProject(h.options.StateDir, id); err != nil {
			h.fail(w, r, err)
			return
		}
		runID = id
		if value := r.URL.Query().Get("after"); value != "" {
			offset, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				http.Error(w, "after must be a log offset", http.StatusBadRequest)
				return
			}
			after = &offset
		}
	}
	if h.streams.Add(1) > maxStreams {
		h.streams.Add(-1)
		http.Error(w, "too many open streams", http.StatusServiceUnavailable)
		return
	}
	defer h.streams.Add(-1)

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	send := func(event string, data any) bool {
		payload, err := json.Marshal(data)
		if err != nil {
			return false
		}
		_ = controller.SetWriteDeadline(time.Now().Add(streamWriteLimit))
		if event == "" {
			_, err = io.WriteString(w, ": keepalive\n\n")
		} else {
			_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
		}
		return err == nil && controller.Flush() == nil
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	lines := make(chan logLine)
	logDone := make(chan struct{})
	if runID > 0 {
		go func() {
			defer close(logDone)
			out := &lineWriter{ctx: ctx, lines: lines}
			if after != nil {
				out.offset = *after
			}
			_ = app.FollowLog(ctx, h.options.StateDir, runID, after, nil, out, io.Discard, io.Discard, false)
			out.flush()
		}()
	}

	current, changed := h.watcher.state()
	if !send("state", h.stateEvent(current)) {
		return
	}
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.done:
			return
		case <-changed:
			current, changed = h.watcher.state()
			if !send("state", h.stateEvent(current)) {
				return
			}
		case line := <-lines:
			if !send("log", line) {
				return
			}
		case <-logDone:
			logDone = nil
			if !send("end", runID) {
				return
			}
		case <-heartbeat.C:
			if !send("", nil) {
				return
			}
		}
	}
}

type stateEvent struct {
	Version string `json:"version"`
	Seq     uint64 `json:"seq"`
	Failing int    `json:"failing"`
}

func (h *Handler) stateEvent(current snapshot) stateEvent {
	return stateEvent{Version: h.options.Version, Seq: current.Seq, Failing: current.Failing}
}

// lineWriter turns followed log bytes into rendered lines. A line is sent only
// once complete, or at the end of the run.
type lineWriter struct {
	ctx     context.Context
	lines   chan<- logLine
	pending []byte
	// offset is the log offset after the last sent line.
	offset int64
}

// logLine is one rendered line and the offset that resumes after it.
type logLine struct {
	HTML string `json:"html"`
	End  int64  `json:"end"`
}

func (l *lineWriter) Write(data []byte) (int, error) {
	l.pending = append(l.pending, data...)
	for {
		end := bytes.IndexByte(l.pending, '\n')
		if end < 0 {
			return len(data), nil
		}
		if err := l.send(l.pending[:end], end+1); err != nil {
			return 0, err
		}
		l.pending = l.pending[end+1:]
	}
}

func (l *lineWriter) flush() {
	if len(l.pending) > 0 {
		_ = l.send(l.pending, len(l.pending))
		l.pending = nil
	}
}

func (l *lineWriter) send(line []byte, consumed int) error {
	select {
	case l.lines <- logLine{HTML: string(renderANSI(line)), End: l.offset + int64(consumed)}:
		l.offset += int64(consumed)
		return nil
	case <-l.ctx.Done():
		return l.ctx.Err()
	}
}

package web

import (
	"sync"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/store"
)

const watchInterval = 500 * time.Millisecond

// snapshot is what every open tab needs after a change: a sequence to detect
// stale pages and the failing count for the title and favicon.
type snapshot struct {
	Seq     uint64
	Failing int
}

// watcher is the viewer's one state poller. It reads data_version on a
// dedicated connection, so commits from any Noon process wake it, and compares
// ChangeStamp so the scheduler's per-second heartbeat does not.
type watcher struct {
	options Options
	mu      sync.Mutex
	current snapshot
	changed chan struct{}
}

func newWatcher(options Options, done <-chan struct{}) *watcher {
	w := &watcher{options: options, changed: make(chan struct{})}
	go w.run(done)
	return w
}

// state returns the latest snapshot and a channel closed at the next change.
func (w *watcher) state() (snapshot, <-chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.current, w.changed
}

func (w *watcher) run(done <-chan struct{}) {
	var state *store.Store
	defer func() {
		if state != nil {
			state.Close()
		}
	}()
	version, stamp := int64(-1), ""
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	for {
		if state == nil {
			// The database may not exist until the scheduler's first write.
			state, _ = store.OpenExisting(w.options.StateDir)
		}
		if state != nil {
			if next, err := state.DataVersion(); err == nil && next != version {
				version = next
				if nextStamp, err := state.ChangeStamp(); err == nil && nextStamp != stamp {
					stamp = nextStamp
					w.publish()
				}
			}
		}
		select {
		case <-done:
			return
		case <-ticker.C:
		}
	}
}

func (w *watcher) publish() {
	failing := 0
	if view, err := app.Overview(w.options.Input, w.options.Now()); err == nil {
		failing = len(view.Failing)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.current = snapshot{Seq: w.current.Seq + 1, Failing: failing}
	close(w.changed)
	w.changed = make(chan struct{})
}

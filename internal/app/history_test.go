package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

func TestSelectLog(t *testing.T) {
	hookLog := []byte("job-one\njob-two\n[noon: on_failure hook exited 0]\nhook-output\n")
	hookOffset := int64(len("job-one\njob-two\n"))
	tests := []struct {
		name          string
		data          []byte
		active        bool
		query         LogQuery
		want          []byte
		wantNext      int64
		wantTruncated bool
		wantMatches   *int
	}{
		{
			name:     "match context windows merge and retain file line numbers after cursor",
			data:     []byte("before\nhit-two\ncontext-three\nhit-four\nhit-five\ncontext-six\nhit-seven\nafter-eight\n"),
			query:    LogQuery{After: int64Pointer(7), Match: stringPointer("hit"), Context: intPointer(1)},
			want:     []byte("2:hit-two\n3-context-three\n4:hit-four\n5:hit-five\n6-context-six\n7:hit-seven\n8-after-eight\n"),
			wantNext: int64(len("before\nhit-two\ncontext-three\nhit-four\nhit-five\ncontext-six\nhit-seven\nafter-eight\n")), wantMatches: intPointer(4),
		},
		{
			name:          "forward byte cap stops before the next whole line",
			data:          []byte("line-1\nline-2\n"),
			query:         LogQuery{After: int64Pointer(0), LimitBytes: intPointer(10)},
			want:          []byte("line-1\n"),
			wantNext:      int64(len("line-1\n")),
			wantTruncated: true,
		},
		{
			name:          "active leaves unterminated line for the next read",
			data:          []byte("complete\npartial"),
			active:        true,
			want:          []byte("complete\n"),
			wantNext:      int64(len("complete\n")),
			wantTruncated: false,
		},
		{
			name:     "terminal includes unterminated line",
			data:     []byte("complete\npartial"),
			want:     []byte("complete\npartial"),
			wantNext: int64(len("complete\npartial")),
		},
		{
			name:     "job section intersects after cursor before hook boundary",
			data:     hookLog,
			query:    LogQuery{Section: "job", HookOffset: &hookOffset, After: int64Pointer(hookOffset - 4)},
			want:     []byte("two\n"),
			wantNext: hookOffset,
		},
		{
			name:     "hook section intersects after cursor within hook range",
			data:     hookLog,
			query:    LogQuery{Section: "hook", HookOffset: &hookOffset, After: int64Pointer(hookOffset + int64(len("[noon: ")))},
			want:     []byte("on_failure hook exited 0]\nhook-output\n"),
			wantNext: int64(len(hookLog)),
		},
		{
			name:     "empty job intersection keeps cursor absolute and nondecreasing",
			data:     hookLog,
			query:    LogQuery{Section: "job", HookOffset: &hookOffset, After: int64Pointer(int64(len(hookLog)) - 1)},
			want:     []byte{},
			wantNext: int64(len(hookLog)) - 1,
		},
		{
			name:     "hook section is empty without a recorded offset",
			data:     hookLog,
			query:    LogQuery{Section: "hook", After: int64Pointer(4)},
			want:     []byte{},
			wantNext: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectLog(tt.data, tt.active, tt.query)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Bytes, tt.want) || got.Next != tt.wantNext || got.Size != int64(len(tt.data)) || got.Truncated != tt.wantTruncated {
				t.Fatalf("selection = bytes %q, next %d, size %d, truncated %v; want %q, %d, %d, %v", got.Bytes, got.Next, got.Size, got.Truncated, tt.want, tt.wantNext, len(tt.data), tt.wantTruncated)
			}
			if !sameIntPointer(got.MatchedLines, tt.wantMatches) {
				t.Fatalf("matched_lines = %v, want %v", intPointerValue(got.MatchedLines), intPointerValue(tt.wantMatches))
			}
		})
	}

	t.Run("forward limits that cannot consume a source rune are usage errors", func(t *testing.T) {
		for _, test := range []struct {
			name  string
			data  []byte
			limit int
			match *string
		}{
			{name: "formatted prefix only", data: []byte("abc\n"), limit: 2, match: stringPointer(".*")},
			{name: "multibyte rune does not fit", data: []byte("α\n"), limit: 1},
		} {
			t.Run(test.name, func(t *testing.T) {
				selected, err := SelectLog(test.data, false, LogQuery{
					After: int64Pointer(0), Match: test.match, LimitBytes: intPointer(test.limit),
				})
				if failure, ok := err.(*Error); !ok || !failure.Usage {
					t.Fatalf("selection = %+v, err=%v; want usage error", selected, err)
				}
			})
		}
	})

	t.Run("repaired text byte limits preserve raw cursor offsets", func(t *testing.T) {
		input := []byte{0xff, '\n'}
		after, limit := int64(0), 2
		_, err := SelectLog(input, false, LogQuery{After: &after, LimitBytes: &limit, RepairUTF8: true})
		if failure, ok := err.(*Error); !ok || !failure.Usage {
			t.Fatalf("two-byte page err=%v; want usage error because the replacement rune needs three bytes", err)
		}

		limit = 3
		first, err := SelectLog(input, false, LogQuery{After: &after, LimitBytes: &limit, RepairUTF8: true})
		if err != nil || string(first.Bytes) != "�" || first.Next != 1 || len(first.Bytes) > limit || !first.Truncated {
			t.Fatalf("first repaired page = %+v, err=%v; want replacement text, raw next 1, and truncation", first, err)
		}
		after = first.Next
		second, err := SelectLog(input, false, LogQuery{After: &after, LimitBytes: &limit, RepairUTF8: true})
		if err != nil || string(second.Bytes) != "\n" || second.Next != 2 || len(second.Bytes) > limit || second.Truncated {
			t.Fatalf("second repaired page = %+v, err=%v; want newline and raw next 2", second, err)
		}
	})

	t.Run("explicitly included active partial line", func(t *testing.T) {
		selected, err := SelectLog([]byte("complete\npartial"), true, LogQuery{IncludeActivePartial: true})
		if err != nil || string(selected.Bytes) != "complete\npartial" || selected.Next != int64(len("complete\npartial")) {
			t.Fatalf("active partial selection = %+v, err=%v", selected, err)
		}
	})

	t.Run("forward pages preserve multibyte bytes and advance through long lines", func(t *testing.T) {
		input := []byte("α\n非常に長い行とUTF-8の続き\n終\n")
		limit := 7
		cursor := int64(0)
		var combined []byte
		for page := 0; cursor < int64(len(input)); page++ {
			if page > len(input) {
				t.Fatal("paging did not advance")
			}
			after := cursor
			selected, err := SelectLog(input, false, LogQuery{After: &after, LimitBytes: &limit})
			if err != nil {
				t.Fatal(err)
			}
			if selected.Next <= cursor && cursor < int64(len(input)) {
				t.Fatalf("page did not advance: cursor=%d next=%d output=%q", cursor, selected.Next, selected.Bytes)
			}
			combined = append(combined, selected.Bytes...)
			cursor = selected.Next
		}
		if !bytes.Equal(combined, input) {
			t.Fatalf("concatenated pages = %q, want exact input %q", combined, input)
		}
		if cursor != int64(len(input)) {
			t.Fatalf("final next = %d, want size %d", cursor, len(input))
		}
	})

	t.Run("tail limit keeps newest whole lines and rune-safe suffix for one long line", func(t *testing.T) {
		limit := 7
		selected, err := SelectLog([]byte("first\nsecond\nthird\n"), false, LogQuery{LimitBytes: &limit})
		if err != nil {
			t.Fatal(err)
		}
		if string(selected.Bytes) != "third\n" || !selected.Truncated {
			t.Fatalf("whole-line tail = %q, truncated=%v; want %q, true", selected.Bytes, selected.Truncated, "third\n")
		}

		limit = 6
		selected, err = SelectLog([]byte("αβγδε\n"), false, LogQuery{LimitBytes: &limit})
		if err != nil {
			t.Fatal(err)
		}
		if string(selected.Bytes) != "δε\n" || !selected.Truncated {
			t.Fatalf("long-line tail = %q, truncated=%v; want rune-safe %q, true", selected.Bytes, selected.Truncated, "δε\n")
		}
	})
}

// A hook append can be visible before its offset transaction commits; job-only
// reads must not mistake those bytes for the job's final error output.
func TestReadRunLogSelectionUncommittedHook(t *testing.T) {
	stateDir := t.TempDir()
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	project, err := state.RegisterProject(t.TempDir(), now)
	if err != nil {
		t.Fatal(err)
	}
	run, err := state.CreateRun(store.NewRun{ProjectID: project, Job: "hook", Trigger: "manual", Digest: "test", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	log, err := state.RunLog(project, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("job-error")); err != nil {
		t.Fatal(err)
	}
	logBytes, truncated, err := log.Close()
	if err != nil {
		t.Fatal(err)
	}
	exit := 3
	if err := state.FinishRun(run.ID, project, "hook", store.Outcome{Status: "failed", FinishedAt: now, ExitCode: &exit, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated, HookEvent: "on_failure"}); err != nil {
		t.Fatal(err)
	}
	// Model the exact disk/row snapshot between CompleteHook's append and commit.
	file, err := os.OpenFile(filepath.Join(stateDir, log.Path()), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n[noon: on_failure hook exited 0]\nhook-output\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, selected, err := ReadRunLogSelection(stateDir, run.ID, LogQuery{Section: "job"})
	if err != nil || string(selected.Bytes) != "job-error" || selected.Next != logBytes {
		t.Fatalf("uncommitted hook selection = %+v, err=%v", selected, err)
	}
}

func TestRunsReportOrphansWithoutChangingStoredStatus(t *testing.T) {
	stateDir := t.TempDir()
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	projectPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	projectID, err := state.RegisterProject(projectPath, now)
	if err != nil {
		t.Fatal(err)
	}
	liveIdentity, err := runner.ProcessStartIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	stale, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: "stale", Trigger: "manual", Digest: "test", Now: now, OwnerPID: os.Getpid(), OwnerStart: "stale-owner-identity"})
	if err != nil {
		t.Fatal(err)
	}
	live, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: "live", Trigger: "manual", Digest: "test", Now: now.Add(time.Second), OwnerPID: os.Getpid(), OwnerStart: liveIdentity})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: "terminal", Trigger: "manual", Digest: "test", Now: now.Add(2 * time.Second), OwnerPID: os.Getpid(), OwnerStart: liveIdentity})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.FinishRun(terminal.ID, projectID, terminal.Job, store.Outcome{Status: "succeeded", FinishedAt: now.Add(3 * time.Second)}); err != nil {
		t.Fatal(err)
	}

	runs, err := RunsWithStatuses(stateDir, projectPath, "", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[int64]Run, len(runs))
	for _, run := range runs {
		byID[run.ID] = run
	}
	if run := byID[stale.ID]; run.Status != "running" || !run.Orphaned {
		t.Fatalf("stale owner mapping = %+v; want running and orphaned", run)
	}
	if run := byID[live.ID]; run.Status != "running" || run.Orphaned {
		t.Fatalf("live owner mapping = %+v; want running and not orphaned", run)
	}
	if run := byID[terminal.ID]; run.Status != "succeeded" || run.Orphaned {
		t.Fatalf("terminal mapping = %+v; want succeeded and not orphaned", run)
	}
	stored, err := state.RunByID(stale.ID)
	if err != nil || stored.Status != "running" {
		t.Fatalf("inspection mutated stale run: %+v err=%v", stored, err)
	}
}

func int64Pointer(value int64) *int64    { return &value }
func intPointer(value int) *int          { return &value }
func stringPointer(value string) *string { return &value }

func sameIntPointer(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func intPointerValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

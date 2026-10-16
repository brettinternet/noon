package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/schedule"
)

// These fixtures prevent accidental YAML re-encoding from changing indentation,
// comments, scalar blocks, line endings, or unrelated setting bytes.
func TestEditJobs(t *testing.T) {
	fixtures := []struct {
		name   string
		source []byte
	}{
		{"four-space indentation", []byte("version: 1\njobs:\n    first:\n        run: echo\n")},
		{"blank lines between jobs", []byte("version: 1\njobs:\n  first:\n    run: echo\n\n  last:\n    run: echo\n")},
		{"comments", []byte("version: 1\n# job collection\njobs:\n  # first head\n  first: # inline key\n    run: echo # inline field\n  # trailing column-one comment follows\n# trailing root comment\n")},
		{"last block scalar", []byte("version: 1\njobs:\n  first:\n    run: |\n      echo keep\n")},
		{"CRLF", []byte("version: 1\r\njobs:\r\n  first:\r\n    run: [echo, keep]\r\n")},
		{"mixed LF and CRLF", []byte("version: 1\r\njobs:\n  first:\n    run: [echo, keep]\r\n# tail\n")},
		{"keep scalar trailing blanks", []byte("jobs:\n  first:\n    run: |+\n      echo keep\n\n\n")},
		{"folded scalar before root trailer", []byte("jobs:\n  first:\n    run: >+\n      echo keep\n\n# tail\ntimezone: UTC\n")},
		{"scalar after separated jobs", []byte("jobs:\n  prior:\n    run: prior\n\n  first:\n    run: |+\n      echo keep\n")},
		{"description scalar trailing blanks", []byte("jobs:\n  first:\n    run: echo\n    description: |+\n      keep\n\n")},
		{"without jobs", []byte("version: 1\ntimezone: UTC\n# tail\n")},
		{"whitespace-only source refused", []byte("  \n")},
	}
	job := config.Job{Name: "added", Description: "Quoted: value", Run: []string{"echo", "quoted value"}, Enabled: true}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			added, err := config.AddJob(fixture.source, "added", job)
			if fixture.name == "whitespace-only source refused" {
				if err == nil {
					t.Fatal("whitespace-only source accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fixture.name == "four-space indentation" && !bytes.Contains(added, []byte("        description:")) {
				t.Fatalf("new fields did not match existing field indentation: %q", added)
			}
			if fixture.name == "blank lines between jobs" && !bytes.Contains(added, []byte("run: echo\n\n  added:")) {
				t.Fatalf("required blank separator missing: %q", added)
			}
			loadBytes(t, added)
			names, err := config.JobNames(fixture.source)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range names {
				before, err := config.JobFromSource(fixture.source, name, nil, "", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				after, err := config.JobFromSource(added, name, nil, "", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if before.Digest != after.Digest || before.Description != after.Description || before.Enabled != after.Enabled {
					t.Fatalf("existing raw job changed: before=%+v after=%+v", before, after)
				}
			}
			removed, err := config.RemoveJob(added, "added")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(removed, fixture.source) {
				t.Fatalf("remove did not restore source bytes\nwant %q\ngot  %q", fixture.source, removed)
			}
		})
	}

	t.Run("all job fields are encoded", func(t *testing.T) {
		root := t.TempDir()
		sched, err := schedule.Parse("every 30m")
		if err != nil {
			t.Fatal(err)
		}
		description := "nightly: check"
		job := config.Job{Name: "all", Description: description, Schedule: &sched, Run: "echo ok", CWD: "subdir", Env: map[string]string{"TOKEN": "secret"}, Timeout: "2m", Overlap: "queue", Missed: "skip", Enabled: false}
		data, err := config.AddJob([]byte("jobs:\n  prior:\n    run: prior\n"), "all", job)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".noon.yaml"), data, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatal(err)
		}
		got := loaded.Jobs["all"]
		if got.Description != description || got.Schedule.String() != "every 30m" || got.Run != "echo ok" || got.CWD != filepath.Join(loaded.Project, "subdir") || got.Env["TOKEN"] != "secret" || got.Timeout != "2m0s" || got.Overlap != "queue" || got.Missed != "skip" || got.Enabled {
			t.Fatalf("fields not serialized: %+v", got)
		}
	})

	t.Run("empty source gets version and jobs", func(t *testing.T) {
		added, err := config.AddJob(nil, "added", job)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(added), "version: 1\njobs:\n") {
			t.Fatalf("empty file output: %q", added)
		}
		loadBytes(t, added)
		removed, err := config.RemoveJob(added, "added")
		if err != nil {
			t.Fatal(err)
		}
		// Empty files gain a version declaration; removing a job preserves it.
		if string(removed) != "version: 1\n" {
			t.Fatalf("empty-source removal: %q", removed)
		}
		loadBytes(t, removed)
	})

	middle := []byte("jobs:\n  first:\n    run: first\n  # second head\n  second:\n    run: second\n  # next head\n  third:\n    run: third\n")
	got, err := config.RemoveJob(middle, "second")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("jobs:\n  first:\n    run: first\n  # next head\n  third:\n    run: third\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("middle removal: got %q want %q", got, want)
	}
	only := []byte("version: 1\njobs:\n  only:\n    run: only\n")
	got, err = config.RemoveJob(only, "only")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "version: 1\n" {
		t.Fatalf("only-job removal: %q", got)
	}
	if _, err := config.AddJob([]byte("version: 1\njobs: {}\n"), "added", job); err == nil || !strings.Contains(err.Error(), "config_edit") {
		t.Fatalf("flow-style jobs mapping error: %v", err)
	}
}

func loadBytes(t *testing.T, data []byte) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".noon.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(config.Input{CWD: root}); err != nil {
		t.Fatalf("edited result did not load: %v\n%s", err, data)
	}
}

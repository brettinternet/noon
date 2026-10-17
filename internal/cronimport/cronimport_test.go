package cronimport_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/cronimport"
)

func TestImportCrontab(t *testing.T) {
	isolateConfigEnvironment(t)

	t.Run("macros", func(t *testing.T) {
		input := "@hourly echo hourly\n@daily echo daily\n@midnight echo midnight\n@weekly echo weekly\n@monthly echo monthly\n@yearly echo yearly\n@annually echo annually\n"
		jobs, err := cronimport.Parse(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"@hourly", "@daily", "@midnight", "@weekly", "@monthly", "@yearly", "@annually"}
		if len(jobs) != len(want) {
			t.Fatalf("got %d jobs, want %d: %+v", len(jobs), len(want), jobs)
		}
		for i, schedule := range want {
			if jobs[i].Schedule != schedule || jobs[i].Run != "echo "+strings.TrimPrefix(schedule, "@") {
				t.Errorf("job %d = %+v, want schedule %q", i, jobs[i], schedule)
			}
		}
	})

	t.Run("quoted environment lines apply to following entries", func(t *testing.T) {
		input := "FOO='two words'\nBAR=\"literal $HOME\"\n0 2 * * * /usr/bin/backup --flag  x\nFOO=next\n0 3 * * * echo second\n"
		jobs, err := cronimport.Parse(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) != 2 || jobs[0].Run != "/usr/bin/backup --flag  x" || jobs[0].Line != 3 {
			t.Fatalf("first entry: %+v", jobs)
		}
		if !reflect.DeepEqual(jobs[0].Env, map[string]string{"FOO": "two words", "BAR": "literal $HOME"}) {
			t.Fatalf("first environment: %#v", jobs[0].Env)
		}
		if !reflect.DeepEqual(jobs[1].Env, map[string]string{"FOO": "next", "BAR": "literal $HOME"}) {
			t.Fatalf("second environment: %#v", jobs[1].Env)
		}
	})

	t.Run("assignment whitespace and numeric commands", func(t *testing.T) {
		jobs, err := cronimport.Parse(strings.NewReader("FOO= \"two words\"\nBAR = ' padded ' \n0 2 * * * 7 --backup\n"))
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) != 1 || jobs[0].Name != "cron-7" || jobs[0].Run != "7 --backup" || jobs[0].Schedule != "0 2 * * *" {
			t.Fatalf("numeric command: %+v", jobs)
		}
		if !reflect.DeepEqual(jobs[0].Env, map[string]string{"FOO": "two words", "BAR": " padded "}) {
			t.Fatalf("assignment whitespace: %#v", jobs[0].Env)
		}
	})

	t.Run("each refused feature names its line", func(t *testing.T) {
		for _, tc := range []struct {
			name, line, message string
		}{
			{"reboot", "@reboot echo hi", "@reboot"},
			{"percent", "0 0 * * * echo before%after", "%"},
			{"mailto", "MAILTO=owner@example.test", "MAILTO"},
			{"shell", "SHELL=/bin/bash", "SHELL"},
			{"cron timezone", "CRON_TZ=UTC", "CRON_TZ"},
			{"timezone", "TZ=UTC", "TZ"},
			{"reserved environment", "NOON_SECRET=value", "NOON_"},
			{"invalid schedule", "0 0 30 2 * echo impossible", "schedule"},
			{"seconds field", "0 0 0 * * * echo seconds", "six-field"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := cronimport.Parse(strings.NewReader("# first line\n" + tc.line + "\n"))
				if err == nil || !strings.Contains(err.Error(), "line 2:") || !strings.Contains(err.Error(), tc.message) {
					t.Fatalf("error = %v, want line 2 and %q", err, tc.message)
				}
			})
		}
		jobs, err := cronimport.Parse(strings.NewReader("MAILTO=\"\"\nSHELL='/bin/sh'\n0 0 * * * echo ok\n"))
		if err != nil || len(jobs) != 1 {
			t.Fatalf("accepted empty MAILTO and /bin/sh: jobs=%+v err=%v", jobs, err)
		}
	})

	t.Run("all errors are reported together", func(t *testing.T) {
		input := "# header\n@reboot echo boot\n0 0 * * * echo percent%stdin\nTZ=UTC\n"
		jobs, err := cronimport.Parse(strings.NewReader(input))
		if err == nil || jobs != nil {
			t.Fatalf("jobs=%+v error=%v, want no partial jobs and an error", jobs, err)
		}
		for _, message := range []string{"line 2: @reboot", "line 3: %", "line 4: TZ"} {
			if !strings.Contains(err.Error(), message) {
				t.Errorf("missing %q in %v", message, err)
			}
		}
	})

	t.Run("names normalize and deduplicate within 64 characters", func(t *testing.T) {
		long := strings.Repeat("a", 64)
		input := strings.Join([]string{
			"0 1 * * * /usr/bin/_backup",
			"0 2 * * * /usr/bin/7z",
			"0 3 * * * /first/foo",
			"0 4 * * * /second/foo",
			"0 5 * * * /first/" + long,
			"0 6 * * * /second/" + long + "b",
			"0 7 * * * /bin/-_",
		}, "\n")
		jobs, err := cronimport.Parse(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"backup", "cron-7z", "foo", "foo-2", long, strings.Repeat("a", 62) + "-2", "cron-7"}
		for i, name := range want {
			if jobs[i].Name != name || len(jobs[i].Name) > 64 {
				t.Errorf("job %d name=%q, want %q (length %d)", i, jobs[i].Name, name, len(jobs[i].Name))
			}
		}
		collisions, err := cronimport.ParseWithExisting(strings.NewReader("0 1 * * * /bin/backup\n0 2 * * * /other/backup\n"), []string{"backup"})
		if err != nil {
			t.Fatal(err)
		}
		if got := []string{collisions[0].Name, collisions[1].Name}; !reflect.DeepEqual(got, []string{"backup-2", "backup-3"}) {
			t.Fatalf("existing-name collisions: %v", got)
		}
	})

	t.Run("rendered preview loads through config", func(t *testing.T) {
		jobs, err := cronimport.Parse(strings.NewReader("TOKEN='a value'\n0 2 * * * /usr/local/bin/backup.sh --daily\n0 3 * * * true\n"))
		if err != nil {
			t.Fatal(err)
		}
		preview, err := cronimport.RenderYAML(jobs)
		if err != nil {
			t.Fatal(err)
		}
		for _, comment := range []string{"# crontab line 2", "# Cron never catches up", "# Cron has no job timeout"} {
			if !strings.Contains(string(preview), comment) {
				t.Errorf("preview missing %q:\n%s", comment, preview)
			}
		}
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".git", "noon.yaml"), preview, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatalf("preview did not load: %v\n%s", err, preview)
		}
		job := cfg.Jobs["backup-sh"]
		if job.Schedule == nil || job.Schedule.String() != "0 2 * * *" || job.Run != "/usr/local/bin/backup.sh --daily" || job.Env["TOKEN"] != "a value" || job.Missed != "skip" || job.Timeout != "none" {
			t.Fatalf("loaded imported job: %+v", job)
		}
		// A YAML-reserved name must render as a string key.
		if cfg.Jobs["true"].Run != "true" {
			t.Fatalf("reserved job name did not load: %v", cfg.Jobs)
		}
	})
}

func FuzzImportCrontab(f *testing.F) {
	f.Add("0 2 * * * /usr/bin/backup\n")
	f.Add("FOO='two words'\n@daily echo ok\n")
	f.Add("@reboot echo no\n0 0 * * * echo percent%stdin\n")
	f.Add("\xff\n")
	f.Add("0 0 * * * null\n")
	home := f.TempDir()
	f.Setenv("HOME", home)
	f.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	f.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	f.Fuzz(func(t *testing.T, input string) {
		jobs, err := cronimport.Parse(strings.NewReader(input))
		if err != nil {
			return
		}
		preview, err := cronimport.RenderYAML(jobs)
		if err != nil {
			t.Fatalf("render successful parse: %v", err)
		}
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".git", "noon.yaml"), preview, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Load(config.Input{CWD: root, StateDirDefault: filepath.Join(root, "state")}); err != nil {
			t.Fatalf("successful output did not load: %v\n%s", err, preview)
		}
	})
}

func isolateConfigEnvironment(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
}

package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"reflect"
	"slices"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
)

func sandbox(t *testing.T) string {
	t.Helper()
	for _, key := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE", "NOON_STATE_DIR"} {
		t.Setenv(key, "") // Register restoration, then isolate from the real environment.
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "user"))
	t.Chdir(root)
	return root
}

func invoke(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{Version: "test"}, &out, &stderr)
	var value map[string]any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatalf("expected one JSON object: %v\nstdout=%s\nstderr=%s", err, &out, &stderr)
	}
	if stderr.Len() != 0 {
		t.Fatalf("JSON diagnostic leaked to stderr: %s", &stderr)
	}
	return code, value
}

func TestInitPreservesExistingConfig(t *testing.T) {
	root := sandbox(t)
	code, result := invoke(t, "--json", "init")
	if code != 0 || result["created"] != true {
		t.Fatalf("first init: %d %+v", code, result)
	}
	path := filepath.Join(root, "noon.yaml")
	data := []byte("# keep this comment\ntimezone: UTC\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	code, result = invoke(t, "init", "--json")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || result["created"] != false || !bytes.Equal(data, got) {
		t.Fatalf("init overwrote config: %d %+v %s", code, result, got)
	}
	code, result = invoke(t, "init", "--local", "--json")
	if code != 0 || result["created"] != true {
		t.Fatalf("local init: %d %+v", code, result)
	}
	info, err := os.Stat(filepath.Join(root, ".noon.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("private file mode: %v", info.Mode())
	}
	code, result = invoke(t, "--json", "config")
	if code != 0 || result["config"].(map[string]any)["timezone"] != "UTC" {
		t.Fatalf("local init shadowed inherited settings: %d %+v", code, result)
	}
}

func TestFlagsOverrideEnvironmentAndFiles(t *testing.T) {
	root := sandbox(t)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte("timezone: Europe/London\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOON_TIMEZONE", "UTC")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--json", "config"}, "UTC"},
		{[]string{"config", "--timezone", "America/Chicago", "--json"}, "America/Chicago"},
	} {
		code, result := invoke(t, tc.args...)
		if code != 0 {
			t.Fatalf("command failed: %+v", result)
		}
		if result["config"].(map[string]any)["timezone"] != tc.want {
			t.Fatalf("precedence: %+v", result)
		}
	}
}

func TestJSONFailuresAreSingleObjects(t *testing.T) {
	sandbox(t)
	for _, tc := range []struct {
		args []string
		exit int
		kind string
	}{
		{[]string{"--json", "--bogus"}, 2, "usage"},
		{[]string{"--bogus", "--json"}, 2, "usage"},
		{[]string{"config", "--bogus", "--json"}, 2, "usage"},
		{[]string{"--json=1", "--config", "missing.yaml", "config", "check"}, 1, "config"},
		{[]string{"--json=TRUE", "--config", "missing.yaml", "config", "check"}, 1, "config"},
		{[]string{"-json", "--config", "missing.yaml", "config", "check"}, 1, "config"},
		{[]string{"--bogus", "--json=t"}, 2, "usage"},
		{[]string{"--json", "config", "unknown"}, 2, "usage"},
		{[]string{"--json", "unknown"}, 2, "usage"},
		{[]string{"--json", "--timezone", "Invalid/Zone"}, 1, "config"},
		{[]string{"--json", "--config", "missing.yaml", "config", "check"}, 1, "config"},
		{[]string{"--json", "--timezone=", "config"}, 2, "usage"},
		{[]string{"--json", "init", "unexpected"}, 2, "usage"},
		{[]string{"--json", "init", "--timezone", "UTC"}, 2, "usage"},
		{[]string{"--json", "--project", "missing-directory", "init"}, 1, "config"},
		{[]string{"--json", "--project", "missing-directory"}, 1, "config"},
		{[]string{"--json", "--project", "missing-directory", "status"}, 1, "config"},
	} {
		code, result := invoke(t, tc.args...)
		failure, ok := result["error"].(map[string]any)
		if !ok || code != tc.exit || failure["code"] != tc.kind || failure["message"] == "" || failure["message"] == nil || result["schema_version"] != float64(1) {
			t.Fatalf("args %v: %d %+v", tc.args, code, result)
		}
		if hint, exists := failure["hint"]; exists && hint == "" {
			t.Fatalf("empty hint was not omitted: %+v", failure)
		}
	}
	for _, key := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE", "NOON_STATE_DIR"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")
			code, result := invoke(t, "--json", "config")
			failure, ok := result["error"].(map[string]any)
			if !ok || code != 2 || failure["code"] != "usage" || failure["message"] == "" || failure["message"] == nil {
				t.Fatalf("empty environment value: %d %+v", code, result)
			}
		})
	}
}

func TestValueFlagsAreNotMistakenForJSON(t *testing.T) {
	sandbox(t)
	for _, flag := range []string{"--timezone", "--project", "-C", "--config", "--state-dir"} {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", flag, "--json", "unexpected"}, config.BuildOpts{}, &out, &stderr)
		if code != 2 || out.Len() != 0 || !strings.HasPrefix(stderr.String(), "noon: ") || !strings.Contains(stderr.String(), "\nHint: ") {
			t.Fatalf("%s value misread: exit=%d stdout=%s stderr=%s", flag, code, &out, &stderr)
		}
	}
}

func TestInspectionDoesNotWriteOrExecute(t *testing.T) {
	root := sandbox(t)
	for _, args := range [][]string{{"--json"}, {"status", "--json"}, {"config", "--json"}, {"config", "check", "--json"}} {
		code, result := invoke(t, args...)
		if code != 0 {
			t.Fatalf("inspection: %d %+v", code, result)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("inspection created files: %v", entries)
	}
}

// Guard against executing a manifest during inspection, leaking secrets, or
// accidentally changing the machine-readable contract.
func TestListJSON(t *testing.T) {
	root := sandbox(t)
	data := "timezone: America/Chicago\njobs:\n  zmanual:\n    run: [touch, SHOULD_NOT_EXIST]\n  adaily:\n    schedule: every day at 9am\n    run: touch SHOULD_NOT_EXIST\n    env: {TOKEN: never-print-this-value, OTHER: also-private}\n  bdisabled:\n    schedule: every 1h\n    enabled: false\n    run: 'true'\n  cinterval:\n    schedule: every 1h\n    run: 'true'\n"
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		t.Helper()
		files := map[string]string{}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				files[path] = "<directory>"
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = string(data)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return files
	}
	before := snapshot()
	code, result := invoke(t, "list", "--json")
	if code != 0 || result["schema_version"] != float64(1) {
		t.Fatalf("%d %+v", code, result)
	}
	project := result["project"].(map[string]any)
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 || len(project) != 4 || project["enabled"] != false || project["path"] != canonical || project["timezone"] != "America/Chicago" || project["scheduler"] != "offline" {
		t.Fatal(result)
	}
	jobs := result["jobs"].([]any)
	if len(jobs) != 4 {
		t.Fatal(jobs)
	}
	wantKeys := strings.Fields("name description schedule next_at run prompt prompt_file cwd env_keys timeout overlap missed enabled paused last_run source shared overrides digest approval")
	slices.Sort(wantKeys)
	for i, name := range []string{"adaily", "bdisabled", "cinterval", "zmanual"} {
		job := jobs[i].(map[string]any)
		keys := make([]string, 0, len(job))
		for key := range job {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if !reflect.DeepEqual(keys, wantKeys) || job["name"] != name || job["cwd"] != canonical || job["source"] != filepath.Join(canonical, "noon.yaml") || job["shared"] != true || job["overrides"] != false || job["timeout"] != "1h" || job["overlap"] != "skip" || job["missed"] != "run_once" || !strings.HasPrefix(job["digest"].(string), "sha256:") {
			t.Fatalf("job shape: %+v", job)
		}
		if i == 1 || i == 3 {
			if job["next_at"] != nil {
				t.Fatal(job)
			}
		} else {
			if _, err := time.Parse(time.RFC3339, job["next_at"].(string)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if jobs[3].(map[string]any)["schedule"] != nil || !reflect.DeepEqual(jobs[3].(map[string]any)["run"], []any{"touch", "SHOULD_NOT_EXIST"}) || jobs[0].(map[string]any)["run"] != "touch SHOULD_NOT_EXIST" || !reflect.DeepEqual(jobs[0].(map[string]any)["env_keys"], []any{"OTHER", "TOKEN"}) {
		t.Fatal(jobs)
	}

	// The shared operation supplies deterministic timezone and interval behavior.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	view, err := app.Inspect(config.Input{CWD: root}, now)
	if err != nil {
		t.Fatal(err)
	}
	if view.Jobs[0].NextAt.Format(time.RFC3339) != "2026-01-01T09:00:00-06:00" || view.Jobs[2].NextAt.Format(time.RFC3339) != "2025-12-31T19:00:00-06:00" {
		t.Fatal(view)
	}
	for _, args := range [][]string{{"list", "--json"}, {"ls"}, {}, {"status"}, {"config", "--json"}, {"config", "check", "--json"}} {
		var out, stderr bytes.Buffer
		if code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr); code != 0 {
			t.Fatalf("%v: %s", args, &stderr)
		}
		for _, secret := range []string{"never-print-this-value", "also-private"} {
			if strings.Contains(out.String()+stderr.String(), secret) {
				t.Fatalf("%v leaked secret", args)
			}
		}
	}
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("inspection changed files: before=%v after=%v", before, after)
	}

	// Invalid aliases must not leak raw environment values through YAML's
	// decoder error, even though parsing fails before a job node is available.
	invalid := "jobs:\n  tests:\n    run: 'true'\n    env:\n      TOKEN: *never-print-this-value\n"
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "check"}, {"list", "--json"}} {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr)
		if code != 1 || strings.Contains(out.String()+stderr.String(), "never-print-this-value") || !strings.Contains(out.String()+stderr.String(), "invalid YAML") {
			t.Fatalf("%v: exit=%d stdout=%s stderr=%s", args, code, &out, &stderr)
		}
	}
	// Unknown repository keys cannot inject terminal escape sequences into
	// the source/path diagnostic during ordinary inspection.
	invalid = "jobs:\n  tests:\n    run: 'true'\n    \"\\e[2J\\e[Hspoof\": ignored\n"
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code = cli.Run(context.Background(), []string{"noon", "config", "check"}, config.BuildOpts{}, &out, &stderr)
	if code != 1 || strings.ContainsRune(stderr.String(), '\x1b') || !strings.Contains(stderr.String(), "unknown field") {
		t.Fatalf("unsafe diagnostic: exit=%d stderr=%q", code, stderr.String())
	}
}

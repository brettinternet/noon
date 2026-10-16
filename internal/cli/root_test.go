package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/brettinternet/bump/internal/cli"
	"github.com/brettinternet/bump/internal/config"
)

func sandbox(t *testing.T) string {
	t.Helper()
	for _, key := range []string{"BUMP_PROJECT", "BUMP_CONFIG", "BUMP_TIMEZONE", "BUMP_STATE_DIR"} {
		t.Setenv(key, "") // Register restoration, then isolate from the real environment.
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "user"))
	t.Chdir(root)
	return root
}

func invoke(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), append([]string{"bump"}, args...), config.BuildOpts{Version: "test"}, &out, &stderr)
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
	path := filepath.Join(root, "bump.yaml")
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
	info, err := os.Stat(filepath.Join(root, ".bump.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("private file mode: %v", info.Mode())
	}
}

func TestFlagsOverrideEnvironmentAndFiles(t *testing.T) {
	root := sandbox(t)
	if err := os.WriteFile(filepath.Join(root, "bump.yaml"), []byte("timezone: Europe/London\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BUMP_TIMEZONE", "UTC")
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
	for _, args := range [][]string{
		{"--json", "--bogus"}, {"--bogus", "--json"}, {"config", "--bogus", "--json"},
		{"--json", "config", "unknown"}, {"--json", "--timezone", "Invalid/Zone"},
		{"--json", "--config", "missing.yaml", "config", "check"},
		{"--json", "--timezone=", "config"}, {"--json", "init", "unexpected"},
	} {
		code, result := invoke(t, args...)
		if code == 0 || result["error"] == nil {
			t.Fatalf("args %v: %d %+v", args, code, result)
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

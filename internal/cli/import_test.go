package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
)

func TestImportWrite(t *testing.T) {
	root := sandbox(t)
	configPath := filepath.Join(root, ".git", "noon.yaml")
	original := []byte("# keep root comment\nversion: 1\njobs:\n  # keep existing job comment\n  backup:\n    run: echo existing\n")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	crontabPath := filepath.Join(root, "crontab.txt")
	if err := os.WriteFile(crontabPath, []byte("TOKEN=secret\n0 2 * * * /usr/local/bin/backup\n0 3 * * * /other/backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, result := invoke(t, "--json", "import", "crontab", crontabPath, "--write")
	if code != 0 || result["written"] != true {
		t.Fatalf("import: exit=%d result=%+v", code, result)
	}
	jobs := result["jobs"].([]any)
	if len(jobs) != 2 {
		t.Fatalf("JSON jobs: %+v", jobs)
	}
	first := jobs[0].(map[string]any)
	if first["name"] != "backup-2" || first["schedule"] != "0 2 * * *" || first["run"] != "/usr/local/bin/backup" || first["line"] != float64(2) || !reflectStringSlice(first["env_keys"], []string{"TOKEN"}) {
		t.Fatalf("JSON import job: %+v", first)
	}
	written, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range []string{
		"# keep root comment", "# keep existing job comment", "# crontab line 2",
		"# crontab line 3", "# Cron never catches up", "# Cron has no job timeout",
	} {
		if !strings.Contains(string(written), comment) {
			t.Errorf("written manifest lost comment %q:\n%s", comment, written)
		}
	}
	loaded, err := config.Load(config.Input{CWD: root})
	if err != nil {
		t.Fatalf("written manifest did not load: %v", err)
	}
	for _, name := range []string{"backup", "backup-2", "backup-3"} {
		if _, exists := loaded.Jobs[name]; !exists {
			t.Errorf("written job %q missing", name)
		}
	}
	if loaded.Jobs["backup-2"].Env["TOKEN"] != "secret" || loaded.Jobs["backup-2"].Missed != "skip" || loaded.Jobs["backup-2"].Timeout != "none" {
		t.Fatalf("written imported definition: %+v", loaded.Jobs["backup-2"])
	}

	beforeFailure := append([]byte(nil), written...)
	if err := os.WriteFile(crontabPath, []byte("0 4 * * * /bin/would-be-added\n@reboot echo unsupported\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, result = invoke(t, "--json", "import", "crontab", crontabPath, "--write")
	failure := result["error"].(map[string]any)
	if code != 1 || !strings.Contains(failure["message"].(string), "line 2: @reboot") {
		t.Fatalf("invalid import: exit=%d result=%+v", code, result)
	}
	afterFailure, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterFailure, beforeFailure) {
		t.Fatalf("failed import changed .git/noon.yaml\nbefore: %s\nafter: %s", beforeFailure, afterFailure)
	}
}

func TestImportWriteExplicitConfig(t *testing.T) {
	root := sandbox(t)
	custom := filepath.Join(root, "custom.yaml")
	customBytes := []byte("version: 1\n")
	if err := os.WriteFile(custom, customBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	crontabPath := filepath.Join(root, "crontab.txt")
	if err := os.WriteFile(crontabPath, []byte("0 2 * * * /bin/backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, result := invoke(t, "--json", "--config", custom, "import", "crontab", crontabPath, "--write")
	failure := result["error"].(map[string]any)
	if code != 2 || failure["code"] != "explicit_config" {
		t.Fatalf("explicit config: exit=%d result=%+v", code, result)
	}
	if got, err := os.ReadFile(custom); err != nil || !bytes.Equal(got, customBytes) {
		t.Fatalf("explicit config changed: %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "noon.yaml")); !os.IsNotExist(err) {
		t.Fatalf("explicit config import wrote local file: %v", err)
	}
}

func TestImportPreview(t *testing.T) {
	root := sandbox(t)
	code, stdout, stderr := invokeTextWithStdin(t, "0 2 * * * /bin/backup\n", "import", "crontab", "-")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "jobs:\n") || !strings.Contains(stdout, "# crontab line 1") {
		t.Fatalf("preview: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if entries := projectFiles(t, root); len(entries) != 0 {
		t.Fatalf("preview wrote project files: %v", entries)
	}
}

func TestImportUsage(t *testing.T) {
	root := sandbox(t)
	code, stdout, stderr := invokeText(t, "import", "crontab")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "crontab -l | noon import crontab -") {
		t.Fatalf("crontab usage: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = invokeText(t, "import")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "crontab") {
		t.Fatalf("importer list: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if entries := projectFiles(t, root); len(entries) != 0 {
		t.Fatalf("usage command changed project: %v", entries)
	}
}

func reflectStringSlice(value any, expected []string) bool {
	items, ok := value.([]any)
	if !ok || len(items) != len(expected) {
		return false
	}
	for i, expected := range expected {
		if items[i] != expected {
			return false
		}
	}
	return true
}

func invokeTextWithStdin(t *testing.T, input string, args ...string) (int, string, string) {
	t.Helper()
	previous := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdin = reader
	defer func() {
		os.Stdin = previous
		_ = reader.Close()
	}()
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

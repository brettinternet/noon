package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUserJobsCLIFlow(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	stateHome := filepath.Join(root, "xdg-state")
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	environment := isolatedEnvironment(home, configHome, stateHome, stateDir)
	userProject := filepath.Join(configHome, "noon")
	run := func(env []string, args ...string) (int, string) {
		t.Helper()
		command := exec.Command(noonBinary, args...)
		command.Dir, command.Env = home, env
		output, err := command.CombinedOutput()
		if err == nil {
			return 0, string(output)
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), string(output)
		}
		t.Fatalf("noon %v: %v\n%s", args, err, output)
		return -1, string(output)
	}
	decode := func(output string) map[string]any {
		t.Helper()
		var result map[string]any
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("decode JSON %q: %v", output, err)
		}
		return result
	}

	code, output := run(environment, "--user")
	if code != 0 || !strings.Contains(output, "user ·") || !strings.Contains(output, "Next: noon --user add") {
		t.Fatalf("empty user dashboard: exit=%d output=%s", code, output)
	}
	for _, path := range []string{configHome, stateHome, stateDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("read-only user dashboard created %s: %v", path, err)
		}
	}

	code, output = run(environment, "--user", "add", "where", "every day at 8am", "--", "pwd")
	if code != 0 || !strings.Contains(output, "noon --user run") {
		t.Fatalf("user add: exit=%d output=%s", code, output)
	}
	for path, want := range map[string]os.FileMode{userProject: 0o700, filepath.Join(userProject, "config.yaml"): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode=%o, want %o", path, info.Mode().Perm(), want)
		}
	}

	code, output = run(environment, "--user", "list", "--json")
	userList := decode(output)
	if code != 0 {
		t.Fatalf("user list: exit=%d output=%s", code, output)
	}
	jobs := userList["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatalf("expected one user job: %+v", userList)
	}
	job := jobs[0].(map[string]any)
	if userList["project"].(map[string]any)["path"] != userProject || job["name"] != "where" || job["shared"] != false || job["approval"] != "not_required" {
		t.Fatalf("user list JSON: %+v", userList)
	}
	code, output = run(environment, "--user", "run", "where")
	if code != 0 || !strings.Contains(output, userProject) {
		t.Fatalf("user run: exit=%d output=%q", code, output)
	}

	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "noon.yaml"), []byte("jobs: {repo-job: {run: 'true'}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, output = run(environment, "-C", repo, "list", "--json")
	repoList := decode(output)
	if code != 0 {
		t.Fatalf("repository list: exit=%d output=%s", code, output)
	}
	for _, item := range repoList["jobs"].([]any) {
		if item.(map[string]any)["name"] == "where" {
			t.Fatalf("user job leaked into repository: %+v", repoList)
		}
	}
	code, output = run(environment, "-C", repo, "run", "where", "--json")
	missing := decode(output)
	if code != 1 || missing["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("repository ran user job: exit=%d output=%s", code, output)
	}

	configFile := filepath.Join(root, "external.yaml")
	if err := os.WriteFile(configFile, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--user", "-C", repo, "list"},
		{"--user", "--config", configFile, "list"},
		{"--user", "add", "x", "--shared", "--", "true"},
		{"--user", "serve"},
	} {
		code, output = run(environment, args...)
		if code != 2 {
			t.Fatalf("user selector conflict %v: exit=%d output=%s", args, code, output)
		}
	}
	projectEnvironment := append(append([]string(nil), environment...), "NOON_PROJECT="+repo)
	code, output = run(projectEnvironment, "--user", "list")
	if code != 2 {
		t.Fatalf("NOON_PROJECT conflict: exit=%d output=%s", code, output)
	}

	code, output = run(environment, "-C", userProject, "list", "--json")
	selectedList := decode(output)
	selectedJobs := selectedList["jobs"].([]any)
	if code != 0 || len(selectedJobs) != 1 || selectedList["project"].(map[string]any)["path"] != userProject {
		t.Fatalf("user path selection differs: exit=%d output=%s", code, output)
	}
	selectedJob := selectedJobs[0].(map[string]any)
	if selectedJob["name"] != job["name"] || selectedJob["shared"] != job["shared"] || selectedJob["approval"] != job["approval"] ||
		!reflect.DeepEqual(selectedJob["run"], job["run"]) || selectedJob["cwd"] != job["cwd"] {
		t.Fatalf("user path job differs: %+v", selectedJob)
	}
}

package cli_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Prevent duplicate or misdirected unattended work, authorization bypass, and
// interpretation of directory names as shell code or event fragments.
func TestShellEntry(t *testing.T) {
	root := sandbox(t)
	for _, key := range []string{"NOON_DEPTH", "NOON_JOB"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	stateDir := filepath.Join(root, "state", "noon")
	project := filepath.Join(root, "project ' $(touch INJECTED)\n")
	manifest := "jobs:\n  entry:\n    run: [touch, EXECUTED]\n  cool:\n    run: 'true'\n    cooldown: 10m\n"
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(project, "noon.yaml"), manifest)
	project, _ = filepath.EvalSymlinks(project)
	sub := filepath.Join(project, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(project, alias); err != nil {
		t.Fatal(err)
	}
	entry := func(job, previous, from, to string, record bool, wantCode int) string {
		t.Helper()
		args := []string{"shell", "entry"}
		if record {
			args = append(args, "--record")
		}
		args = append(args, "--", "bash", job, previous, from, to)
		code, out, stderr := triggerCLI(t, args...)
		if code != wantCode {
			t.Fatalf("entry %q: code=%d output=%s stderr=%s", to, code, out, stderr)
		}
		if code == 0 && len(stderr) != 0 {
			t.Fatalf("no-op/success not quiet: %s", stderr)
		}
		return strings.TrimSuffix(string(out), "\n")
	}
	outside := entry("entry", "", root, root, true, 0)
	initial := entry("entry", outside, root, project, true, 0)
	entry("entry", outside, root, project, false, 0) // unenabled: no state
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("no-op created state: %v", err)
	}
	if code, result := invoke(t, "-C", project, "enable", "--json"); code != 0 {
		t.Fatal(result)
	}
	initial = entry("entry", initial, project, project, true, 0)
	if len(triggerRuns(t, stateDir, project)) != 0 {
		t.Fatal("initialization queued work")
	}
	// The inherited override must not select the unrelated current directory.
	t.Setenv("NOON_PROJECT", root)
	t.Setenv("NOON_CONFIG", filepath.Join(root, "missing.yaml"))
	t.Setenv("NOON_TIMEZONE", "Invalid/Zone")
	current := entry("entry", outside, root, project, false, 0)
	if current != initial {
		t.Fatal("unstable project token")
	}
	runs := triggerRuns(t, stateDir, project)
	if len(runs) != 1 || runs[0].Status != "queued" || runs[0].IdempotencyKey.Valid {
		t.Fatalf("offline queue: %+v", runs)
	}
	assertTriggerEvent(t, stateDir, runs[0], map[string]any{"event": "project_entry", "shell": "bash", "previous_directory": root, "directory": project, "project": project})
	entry("entry", current, project, sub, false, 0)
	entry("entry", current, sub, alias, false, 0)
	entry("entry", current, alias, project, true, 0)
	left := entry("entry", current, project, root, false, 0)
	entry("entry", left, root, project, false, 0) // due-now coalescing
	if len(triggerRuns(t, stateDir, project)) != 1 {
		t.Fatal("same-project/reload/coalescing created duplicates")
	}
	for _, key := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE"} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	if code, result := invoke(t, "-C", project, "cancel", "entry", "--json"); code != 0 {
		t.Fatal(result)
	}
	entry("entry", left, root, project, false, 0)
	if len(triggerRuns(t, stateDir, project)) != 2 {
		t.Fatal("re-entry was permanently deduplicated")
	}
	entry("cool", outside, root, project, false, 0)
	if code, result := invoke(t, "-C", project, "cancel", "cool", "--json"); code != 0 {
		t.Fatal(result)
	}
	entry("cool", outside, root, project, false, 0)
	if len(triggerRunsForJob(t, stateDir, project, "cool")) != 1 {
		t.Fatal("cooldown bypassed")
	}
	entry("missing", outside, root, project, false, 0)
	if code, result := invoke(t, "-C", project, "pause", "entry", "--json"); code != 0 {
		t.Fatal(result)
	}
	entry("entry", outside, root, project, false, 1)
	if code, result := invoke(t, "-C", project, "resume", "entry", "--json"); code != 0 {
		t.Fatal(result)
	}
	write(filepath.Join(project, "noon.yaml"), strings.Replace(manifest, "EXECUTED", "CHANGED", 1))
	entry("entry", outside, root, project, false, 1)
	write(filepath.Join(project, "noon.yaml"), strings.Replace(manifest, "run: [touch, EXECUTED]", "enabled: false\n    run: [touch, EXECUTED]", 1))
	entry("entry", outside, root, project, false, 1)
	write(filepath.Join(project, "noon.yaml"), manifest)
	t.Setenv("NOON_DEPTH", "3")
	entry("entry", outside, root, project, false, 1)
	_ = os.Unsetenv("NOON_DEPTH")
	t.Setenv("NOON_PROJECT", project)
	t.Setenv("NOON_JOB", "entry")
	entry("entry", outside, root, project, false, 1)
	_ = os.Unsetenv("NOON_PROJECT")
	_ = os.Unsetenv("NOON_JOB")
	if len(triggerRunsForJob(t, stateDir, project, "entry")) != 2 {
		t.Fatal("authorization/recursion failure inserted work")
	}
	// Both a nested marker and a worktree .git file are distinct checkouts.
	for _, marker := range []string{"noon.yaml", ".git"} {
		nested := filepath.Join(project, "nested-"+marker)
		if marker == ".git" {
			write(filepath.Join(nested, ".git"), "gitdir: elsewhere\n")
		}
		write(filepath.Join(nested, "noon.yaml"), manifest)
		entry("entry", current, project, nested, false, 0)
		if len(triggerRuns(t, stateDir, nested)) != 0 {
			t.Fatal("nested checkout inherited enablement")
		}
		if code, result := invoke(t, "-C", nested, "enable", "--json"); code != 0 {
			t.Fatal(result)
		}
		entry("entry", current, project, nested, false, 0)
		if len(triggerRuns(t, stateDir, nested)) != 1 {
			t.Fatal("nested boundary not entered")
		}
	}
	// Recorded explicit config must win over both the project's local override
	// and the invocation's NOON_CONFIG. A missing recorded job is a no-op.
	explicit := filepath.Join(root, "approved.yaml")
	write(explicit, "jobs:\n  recorded:\n    run: 'true'\n")
	if code, result := invoke(t, "-C", project, "--config", explicit, "enable", "--json"); code != 0 {
		t.Fatal(result)
	}
	entry("entry", outside, root, project, false, 0)
	entry("recorded", outside, root, project, false, 0)
	if len(triggerRunsForJob(t, stateDir, project, "recorded")) != 1 || len(triggerRunsForJob(t, stateDir, project, "entry")) != 2 {
		t.Fatal("recorded config selection ignored")
	}
	for _, name := range []string{"INJECTED", "EXECUTED", "CHANGED"} {
		if _, err := os.Stat(filepath.Join(project, name)); !os.IsNotExist(err) {
			t.Fatalf("executed %s: %v", name, err)
		}
	}
}

// Completion of mutating commands must stay read-only and must not leak jobs
// from another project/config or offer selectors in flag-value/add-name slots.
func TestShellCompletion(t *testing.T) {
	root := sandbox(t)
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "noon.yaml"), "jobs:\n  local-job:\n    run: [touch, EXECUTED]\n")
	write(filepath.Join(root, "user", "noon", "config.yaml"), "agents:\n  reviewer:\n    command: [agent]\njobs:\n  personal-job: {run: 'true'}\n")
	other := filepath.Join(root, "other $(touch INJECTED)")
	write(filepath.Join(other, "noon.yaml"), "jobs:\n  other-job:\n    run: 'true'\n")
	explicit := filepath.Join(root, "explicit config.yaml")
	write(explicit, "jobs:\n  explicit-job:\n    run: 'true'\n")
	complete := func(args ...string) []string {
		t.Helper()
		code, out, stderr := triggerCLI(t, append(args, "--generate-shell-completion")...)
		if code != 0 || len(stderr) != 0 {
			t.Fatalf("completion %q: %d %s %s", args, code, out, stderr)
		}
		return strings.Fields(string(out))
	}
	for _, name := range []string{"run", "rm", "ls", "shell"} {
		if !slices.Contains(complete(""), name) {
			t.Fatalf("missing command %s", name)
		}
	}
	if !slices.Contains(complete("run", "--d"), "--detach") || !slices.Contains(complete("run", "-"), "-C") {
		t.Fatal("missing flag/alias")
	}
	for command, aliases := range map[string][]string{
		"run": {"-d", "-j"}, "history": {"-n", "-s", "-j"},
		"logs": {"-f", "-s", "-n", "-c", "-m", "-b", "-j"}, "add": {"-a", "-p", "-t", "-j"},
	} {
		for _, alias := range aliases {
			if !slices.Contains(complete(command, "-"), alias) {
				t.Fatalf("missing %s alias %s", command, alias)
			}
		}
	}
	for _, args := range [][]string{{"run", "-d", ""}, {"history", "-n", "5", ""}, {"logs", "-s", "failed", ""}, {"-j", "run", ""}} {
		if got := complete(args...); !reflect.DeepEqual(got, []string{"local-job"}) {
			t.Fatalf("short flag consumed selector %q: %v", args, got)
		}
	}
	for _, args := range [][]string{{"add", "-p", ""}, {"add", "-t", ""}, {"history", "-n", ""}, {"run", "--key", ""}, {"add", "--", "-j", ""}, {"run", "--", "local-job", ""}} {
		if got := complete(args...); len(got) != 0 {
			t.Fatalf("completed a value or literal argv %q: %v", args, got)
		}
	}
	if got := complete("add", "-a", ""); !reflect.DeepEqual(got, []string{"claude", "codex", "reviewer"}) {
		t.Fatalf("agent profiles: %v", got)
	}
	if got := complete("add", "--agent=rev"); !reflect.DeepEqual(got, []string{"reviewer"}) {
		t.Fatalf("equals agent profile: %v", got)
	}
	for _, args := range [][]string{{"add", "--prompt-file", ""}, {"add", "--prompt-file="}} {
		if got := complete(args...); !reflect.DeepEqual(got, []string{"__NOON_COMPLETE_PATH_FILE__"}) {
			t.Fatalf("prompt-file path marker %q: %v", args, got)
		}
	}
	if got := complete("--project", ""); !reflect.DeepEqual(got, []string{"__NOON_COMPLETE_PATH_DIR__"}) {
		t.Fatalf("project directory marker: %v", got)
	}
	for _, args := range [][]string{{"--project=."}, {"-C=."}} {
		if got := complete(args...); !reflect.DeepEqual(got, []string{"__NOON_COMPLETE_PATH_DIR__"}) {
			t.Fatalf("equals project directory marker %q: %v", args, got)
		}
	}
	if got := complete("--config", ""); !reflect.DeepEqual(got, []string{"__NOON_COMPLETE_PATH_FILE__"}) {
		t.Fatalf("config path marker: %v", got)
	}
	if got := complete("run", "--config="); !reflect.DeepEqual(got, []string{"__NOON_COMPLETE_PATH_FILE__"}) {
		t.Fatalf("equals config path marker: %v", got)
	}
	if got := complete("import", "crontab", ""); !reflect.DeepEqual(got, []string{"__NOON_COMPLETE_PATH_FILE__", "-"}) {
		t.Fatalf("crontab path: %v", got)
	}
	if got := complete("shell", "init", ""); !reflect.DeepEqual(got, []string{"bash", "zsh", "fish"}) {
		t.Fatalf("shell names: %v", got)
	}
	statuses := []string{"queued", "running", "succeeded", "failed", "timed_out", "cancelled", "interrupted", "skipped", "missed", "expired"}
	if got := complete("logs", "--status", ""); !reflect.DeepEqual(got, statuses) {
		t.Fatalf("status values: %v", got)
	}
	for _, args := range [][]string{{"history", "-s", "fa"}, {"logs", "--status=fa"}, {"logs", "-s=fa"}} {
		if got := complete(args...); !reflect.DeepEqual(got, []string{"failed"}) {
			t.Fatalf("status prefix %q: %v", args, got)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"history", "--status", "failed,que"}, "failed,queued"},
		{[]string{"logs", "--status=failed,que"}, "failed,queued"},
		{[]string{"logs", "--status", "failed", "-s", "que"}, "queued"},
	} {
		if got := complete(tc.args...); !reflect.DeepEqual(got, []string{tc.want}) {
			t.Fatalf("comma/repeated status %q: %v", tc.args, got)
		}
	}
	if got := complete("logs", "--section", ""); !reflect.DeepEqual(got, []string{"all", "job", "hook"}) {
		t.Fatalf("log sections: %v", got)
	}
	if got := complete("logs", "--section=ho"); !reflect.DeepEqual(got, []string{"hook"}) {
		t.Fatalf("equals log section: %v", got)
	}
	if got := complete("shell", "init", "--on-enter", ""); !reflect.DeepEqual(got, []string{"local-job"}) {
		t.Fatalf("entry job names: %v", got)
	}
	for _, command := range []string{"run", "pause", "resume", "remove", "rm", "history", "logs", "cancel"} {
		if got := complete(command, "local"); !reflect.DeepEqual(got, []string{"local-job"}) {
			t.Fatalf("%s: %v", command, got)
		}
		if got := complete("--project", other, command, ""); !reflect.DeepEqual(got, []string{"other-job"}) {
			t.Fatalf("project: %v", got)
		}
		if got := complete(command, "--config", explicit, ""); !reflect.DeepEqual(got, []string{"explicit-job"}) {
			t.Fatalf("config: %v", got)
		}
		if got := complete(command, "local-job", ""); len(got) != 0 {
			t.Fatalf("extra selector: %v", got)
		}
	}
	for _, args := range [][]string{{"add", ""}, {"run", "--key", ""}, {"run", "--key", "loc"}, {"run", "--key=loc"}, {"run", "--", "local-job", ""}, {"service", "install", ""}, {"enable", ""}, {"init", ""}, {"shell", "entry", ""}} {
		if got := complete(args...); len(got) != 0 {
			t.Fatalf("unexpected candidates %q: %v", args, got)
		}
	}
	t.Setenv("NOON_PROJECT", other)
	if got := complete("run", ""); !reflect.DeepEqual(got, []string{"other-job"}) {
		t.Fatal(got)
	}
	if got := complete("-C", root, "run", ""); !reflect.DeepEqual(got, []string{"local-job"}) {
		t.Fatal(got)
	}
	if got := complete("--user", "run", ""); !reflect.DeepEqual(got, []string{"personal-job"}) {
		t.Fatalf("user completion: %v", got)
	}
	_ = os.Unsetenv("NOON_PROJECT")
	// Real Bash COMP_WORDS retains quote syntax and splits --flag=value.
	t.Setenv("NOON_COMPLETION_SHELL", "bash")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-C", triggerShellQuote(other), "run", ""}, "other-job"},
		{[]string{"--project", "=", triggerShellQuote(other), "run", ""}, "other-job"},
		{[]string{"-C", "=", triggerShellQuote(other), "run", ""}, "other-job"},
		{[]string{"--config=" + triggerShellQuote(explicit), "run", ""}, "explicit-job"},
		{[]string{"--config", "=", triggerShellQuote(explicit), "run", ""}, "explicit-job"},
		{[]string{"-C", strings.ReplaceAll(strings.ReplaceAll(other, " ", "\\ "), "$", "\\$"), "run", ""}, "other-job"},
	} {
		if got := complete(tc.args...); !reflect.DeepEqual(got, []string{tc.want}) {
			t.Fatalf("quoted Bash argv %q: %v", tc.args, got)
		}
	}
	_ = os.Unsetenv("NOON_COMPLETION_SHELL")
	write(filepath.Join(root, "noon.yaml"), "invalid: [")
	if got := complete("run", ""); len(got) != 0 {
		t.Fatal(got)
	}
	if !slices.Contains(complete("run", "--d"), "--detach") || !slices.Contains(complete(""), "run") {
		t.Fatal("broken config suppressed static completion")
	}
	if got := complete("shell", "init", ""); !reflect.DeepEqual(got, []string{"bash", "zsh", "fish"}) {
		t.Fatalf("broken config suppressed shell names: %v", got)
	}
	if got := complete("logs", "--status", ""); !reflect.DeepEqual(got, statuses) {
		t.Fatalf("broken config suppressed statuses: %v", got)
	}
	if got := complete("logs", "--section", ""); !reflect.DeepEqual(got, []string{"all", "job", "hook"}) {
		t.Fatalf("broken config suppressed sections: %v", got)
	}
	for _, args := range [][]string{{"shell"}, {"shell", "init"}, {"shell", "--help"}, {"shell", "init", "bash"}, {"shell", "init", "zsh", "--on-enter", "entry"}, {"shell", "init", "fish"}} {
		if code, out, stderr := triggerCLI(t, args...); code != 0 || len(out) == 0 || len(stderr) != 0 {
			t.Fatalf("init/help %q: %d %s %s", args, code, out, stderr)
		}
	}
	for _, args := range [][]string{{"shell", "init", "--on-enter", "entry"}, {"shell", "init", "sh"}, {"shell", "init", "bash", "extra"}, {"shell", "init", "bash", "--on-enter", "$(touch INJECTED)"}, {"shell", "init", "bash", "--on-enter", ""}, {"shell", "init", "bash", "--json"}} {
		if code, _, _ := triggerCLI(t, args...); code != 2 {
			t.Fatalf("invalid init %q: %d", args, code)
		}
	}
	for _, name := range []string{"state", "EXECUTED", "INJECTED"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("completion/init created %s: %v", name, err)
		}
	}
}

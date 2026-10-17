package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

// Guards against silently trusting changed shared definitions, losing provenance
// through local toggles, and partial approval changes after invalid configuration.
func TestEnableApproval(t *testing.T) {
	project, err := filepath.EvalSymlinks(sandbox(t))
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "noon")
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(project, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base := "jobs:\n  a:\n    run: 'true'\n    schedule: daily\n  b:\n    run: 'true'\n"
	write("noon.yaml", base)
	check := func(enabled bool, expected map[string]string, extra ...string) {
		t.Helper()
		code, result := invoke(t, append([]string{"list", "--json"}, extra...)...)
		if code != 0 || result["project"].(map[string]any)["enabled"] != enabled {
			t.Fatalf("list: %d %+v", code, result)
		}
		got := map[string]string{}
		for _, value := range result["jobs"].([]any) {
			job := value.(map[string]any)
			got[job["name"].(string)] = job["approval"].(string)
		}
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("approvals=%v want=%v", got, expected)
		}
	}
	enable := func(extra ...string) {
		t.Helper()
		code, result := invoke(t, append([]string{"enable", "--json"}, extra...)...)
		if code != 0 || result["project"].(map[string]any)["enabled"] != true {
			t.Fatalf("enable: %d %+v", code, result)
		}
	}
	snapshot := func() store.ProjectApproval {
		t.Helper()
		state, err := store.OpenExisting(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		defer state.Close()
		result, err := state.ApprovalForProject(project)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	check(false, map[string]string{"a": "needs_approval", "b": "needs_approval"})
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("inspection created state: %v", err)
	}
	enable()
	check(true, map[string]string{"a": "approved", "b": "approved"})
	initial := snapshot()
	enable()
	if got := snapshot(); !reflect.DeepEqual(got, initial) {
		t.Fatalf("repeat enable changed state: %+v", got)
	}
	for _, change := range []struct{ from, to string }{
		{"run: 'true'", "run: 'exit 0'"},
		{"schedule: daily", "schedule: weekly"},
		{"schedule: daily", "schedule: daily\n    env: {TOKEN: secret}"},
	} {
		write("noon.yaml", strings.Replace(base, change.from, change.to, 1))
		check(true, map[string]string{"a": "needs_approval", "b": "approved"})
	}
	// Explicit foreground execution remains authorized even when its digest changed.
	var runOut, runErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "run", "a", "--json"}, config.BuildOpts{}, &runOut, &runErr); code != 0 {
		t.Fatalf("foreground: %d %s %s", code, &runOut, &runErr)
	}
	write("noon.yaml", strings.Replace(base, "schedule: daily", "schedule: daily\n    description: changed", 1))
	check(true, map[string]string{"a": "approved", "b": "approved"})
	write(".git/noon.yaml", "jobs:\n  a: {enabled: false}\n  local: {run: 'true'}\n")
	check(true, map[string]string{"a": "approved", "b": "approved", "local": "not_required"})
	write("noon.yaml", strings.Replace(base, "run: 'true'", "run: 'exit 0'", 1))
	check(true, map[string]string{"a": "needs_approval", "b": "approved", "local": "not_required"})
	write(".git/noon.yaml", "jobs:\n  a: {run: 'true'}\n  local: {run: 'true'}\n")
	check(true, map[string]string{"a": "not_required", "b": "approved", "local": "not_required"})
	enable()
	if got := snapshot(); len(got.Approvals) != 1 || got.Approvals["b"] == "" {
		t.Fatalf("obsolete shared approval retained: %+v", got)
	}
	before := snapshot()
	write("noon.yaml", "jobs:\n  b: {run: 'true', invalid: field}\n")
	if code, result := invoke(t, "enable", "--json"); code != 1 {
		t.Fatalf("invalid enable: %d %+v", code, result)
	}
	if after := snapshot(); !reflect.DeepEqual(after, before) {
		t.Fatalf("invalid enable changed approval: %+v", after)
	}
	// Broken shared and local YAML must not prevent revocation or erase history.
	write("noon.yaml", "jobs: [invalid")
	write(".git/noon.yaml", "jobs: [invalid")
	for range 2 {
		if code, result := invoke(t, "disable", "--json"); code != 0 {
			t.Fatalf("disable: %d %+v", code, result)
		}
	}
	write("noon.yaml", base)
	write(".git/noon.yaml", "jobs:\n  a: {run: 'true'}\n  local: {run: 'true'}\n")
	check(false, map[string]string{"a": "not_required", "b": "needs_approval", "local": "not_required"})
	if got := snapshot(); got.Enabled || len(got.Approvals) != 0 {
		t.Fatalf("disable: %+v", got)
	}
	if code, result := invoke(t, "history", "--json"); code != 0 || len(result["runs"].([]any)) != 1 {
		t.Fatalf("lost history: %d %+v", code, result)
	}
	write("explicit.yaml", base)
	enable("--config", "explicit.yaml")
	explicitPath, err := filepath.Abs("explicit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot(); !got.ConfigFile.Valid || got.ConfigFile.String != explicitPath {
		t.Fatalf("config selection: %+v", got)
	}
	check(true, map[string]string{"a": "approved", "b": "approved"}, "--config", "explicit.yaml")
	write("explicit.yaml", strings.Replace(base, "schedule: daily", "schedule: weekly", 1))
	for _, args := range [][]string{{"list"}, {}} {
		var out, stderr bytes.Buffer
		args = append([]string{"noon", "--config", "explicit.yaml"}, args...)
		if code := cli.Run(context.Background(), args, config.BuildOpts{}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "needs approval") || strings.Count(out.String(), "Next: noon enable") != 1 {
			t.Fatalf("notice: %d %s %s", code, &out, &stderr)
		}
	}
	write("explicit.yaml", "jobs: {t: {run: 'true', invalid: field}}\n")
	if code, result := invoke(t, "disable", "--config", "explicit.yaml", "--json"); code != 0 {
		t.Fatalf("disable invalid explicit job: %d %+v", code, result)
	}
	if got := snapshot(); got.Enabled || len(got.Approvals) != 0 {
		t.Fatalf("explicit disable: %+v", got)
	}
	enable()
	if got := snapshot(); got.ConfigFile.Valid {
		t.Fatalf("automatic config not restored: %+v", got)
	}
}

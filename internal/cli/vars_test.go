package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVarsFileCLIResolutionApprovalDoctorAndEnvironment(t *testing.T) {
	root := sandbox(t)
	manifest := filepath.Join(root, "noon.yaml")
	varsFile := filepath.Join(root, "vars.env")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(manifest, `vars_file: vars.env
jobs:
  show:
    schedule: ${SCHEDULE:-every 1h}
    run: [sh, -c, 'test -z "$VARFILE_ONLY_VALUE"']
  keep:
    schedule: ${SCHEDULE:-every 7h}
    run: "true"
`)
	write(varsFile, "SCHEDULE=every 2h\nUNUSED=first\nVARFILE_ONLY_VALUE=vars-secret\n")
	write(filepath.Join(root, ".git", "noon.yaml"), "vars_file: private.vars\njobs:\n  local:\n    run: \"true\"\n")
	write(filepath.Join(root, "private.vars"), "PRIVATE_ONLY=private-value\n")

	if code, result := invoke(t, "--json", "config", "check"); code != 0 || result["valid"] != true {
		t.Fatalf("config check: %d %+v", code, result)
	}
	code, result := invoke(t, "list", "--json")
	if code != 0 {
		t.Fatalf("list: %d %+v", code, result)
	}
	if listSchedule(result, "show") != "every 2h" {
		t.Fatalf("list did not show resolved schedule: %+v", result["jobs"])
	}

	userConfig := filepath.Join(root, "user", "noon", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	write(userConfig, "on_success: [sh, -c, 'printf \"%s\" \"${VARFILE_ONLY_VALUE-unset}\" > hook.out']\n")
	t.Setenv("VARFILE_ONLY_VALUE", "")
	if err := os.Unsetenv("VARFILE_ONLY_VALUE"); err != nil {
		t.Fatal(err)
	}
	if code, result := invoke(t, "--json", "enable"); code != 0 {
		t.Fatalf("enable: %d %+v", code, result)
	}
	runCode, _, _ := invokeText(t, "run", "show")
	if runCode != 0 {
		t.Fatalf("vars_file value reached job environment: code=%d", runCode)
	}
	hook, err := os.ReadFile(filepath.Join(root, "hook.out"))
	if err != nil || string(hook) != "unset" {
		t.Fatalf("vars_file value reached hook environment: %q err=%v", hook, err)
	}
	code, result = invoke(t, "doctor", "--json")
	if code != 0 {
		t.Fatalf("doctor with vars file: %d %+v", code, result)
	}
	found := map[string]bool{}
	for _, raw := range result["checks"].([]any) {
		check := raw.(map[string]any)
		if check["name"] == "vars_file" {
			if check["status"] != "ok" {
				t.Fatalf("vars_file doctor check: %+v", check)
			}
			found[check["message"].(string)] = true
		}
	}
	if !found["vars.env"] || !found["private.vars"] || len(found) != 2 {
		t.Fatalf("doctor did not check each configured vars_file: %+v", result)
	}

	write(varsFile, "SCHEDULE=every 3h\nUNUSED=first\nVARFILE_ONLY_VALUE=vars-secret\n")
	code, result = invoke(t, "list", "--json")
	if code != 0 || listApproval(result, "show") != "needs_approval" {
		t.Fatalf("vars change did not revoke approval: %d %+v", code, result)
	}
	if code, result = invoke(t, "run", "show", "--detach", "--json"); code != 1 || result["error"].(map[string]any)["code"] != "needs_approval" {
		t.Fatalf("unapproved vars change queued: %d %+v", code, result)
	}
	if code, result = invoke(t, "--json", "enable"); code != 0 {
		t.Fatalf("re-enable vars change: %d %+v", code, result)
	}
	if code, result = invoke(t, "list", "--json"); code != 0 || listApproval(result, "show") != "approved" {
		t.Fatalf("vars change remained unapproved: %d %+v", code, result)
	}

	write(manifest, strings.Replace(string(mustRead(t, manifest)), "${SCHEDULE:-every 7h}", "${SCHEDULE:-every 9h}", 1))
	if code, result = invoke(t, "list", "--json"); code != 0 || listApproval(result, "keep") != "approved" {
		t.Fatalf("equivalent expression changed approval: %d %+v", code, result)
	}
	write(varsFile, "SCHEDULE=every 3h\nUNUSED=changed\nVARFILE_ONLY_VALUE=vars-secret\n")
	if code, result = invoke(t, "list", "--json"); code != 0 || listApproval(result, "keep") != "approved" {
		t.Fatalf("unused var changed approval: %d %+v", code, result)
	}
	write(manifest, `vars_file: vars.env
jobs:
  show:
    schedule: every 4h
    run: [sh, -c, 'test -z "$VARFILE_ONLY_VALUE"']
  keep:
    schedule: ${SCHEDULE:-every 9h}
    run: "true"
`)
	if code, result = invoke(t, "list", "--json"); code != 0 || listApproval(result, "show") != "needs_approval" {
		t.Fatalf("inline schedule change did not revoke approval: %d %+v", code, result)
	}

	code, out, stderr := invokeText(t, "add", "added", "${SCHEDULE:-every 7h}", "--sh", "true", "--shared")
	if code != 0 || stderr != "" || !strings.Contains(out, "every 3h") {
		t.Fatalf("add interpolation: code=%d stdout=%q stderr=%q", code, out, stderr)
	}
	addedSource := string(mustRead(t, filepath.Join(root, "noon.yaml")))
	if !strings.Contains(addedSource, "schedule: ${SCHEDULE:-every 7h}") || !strings.Contains(addedSource, "schedule: ${SCHEDULE:-every 9h}") || !strings.Contains(addedSource, "vars_file: vars.env") {
		t.Fatalf("add did not preserve expressions and vars_file:\n%s", addedSource)
	}
	code, result = invoke(t, "list", "--json")
	if code != 0 || listSchedule(result, "added") != "every 3h" || listApproval(result, "added") != "approved" {
		t.Fatalf("added expression is not resolved and approved on load: %d %+v", code, result)
	}
	if code, out, stderr = invokeText(t, "remove", "added", "--shared"); code != 0 || stderr != "" {
		t.Fatalf("remove vars-file job: %d %q %q", code, out, stderr)
	}
	removedSource := string(mustRead(t, filepath.Join(root, "noon.yaml")))
	if !strings.Contains(removedSource, "vars_file: vars.env") || !strings.Contains(removedSource, "schedule: every 4h") || !strings.Contains(removedSource, "schedule: ${SCHEDULE:-every 9h}") || strings.Contains(removedSource, "schedule: ${SCHEDULE:-every 7h}") {
		t.Fatalf("remove did not preserve surrounding source:\n%s", removedSource)
	}

	write(manifest, "vars_file: absent.vars\njobs:\n  missing:\n    schedule: ${REQUIRED_SCHEDULE}\n    run: \"true\"\n")
	code, result = invoke(t, "config", "check", "--json")
	failure, _ := result["error"].(map[string]any)
	if code != 1 || failure["message"] == nil || !strings.Contains(failure["message"].(string), "REQUIRED_SCHEDULE") || !strings.Contains(failure["message"].(string), "absent.vars") {
		t.Fatalf("missing required variable config check: %d %+v", code, result)
	}
	write(manifest, "vars_file: absent.vars\njobs:\n  missing:\n    schedule: ${REQUIRED_SCHEDULE:-every 1h}\n    run: \"true\"\n")
	code, result = invoke(t, "doctor", "--json")
	if code != 0 {
		t.Fatalf("doctor with missing vars file: %d %+v", code, result)
	}
	warned := false
	for _, raw := range result["checks"].([]any) {
		check := raw.(map[string]any)
		if check["name"] == "vars_file" {
			warned = warned || check["status"] == "warn" && strings.Contains(check["message"].(string), "absent.vars") && strings.Contains(check["message"].(string), "using expression defaults")
		}
	}
	if !warned {
		t.Fatalf("doctor did not warn about absent vars file: %+v", result)
	}
}

func listApproval(result map[string]any, name string) string {
	for _, raw := range result["jobs"].([]any) {
		job := raw.(map[string]any)
		if job["name"] == name {
			return job["approval"].(string)
		}
	}
	return "missing"
}

func listSchedule(result map[string]any, name string) string {
	for _, raw := range result["jobs"].([]any) {
		job := raw.(map[string]any)
		if job["name"] == name {
			return fmt.Sprint(job["schedule"])
		}
	}
	return "missing"
}

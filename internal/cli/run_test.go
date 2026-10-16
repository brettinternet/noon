package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/bump/internal/cli"
	"github.com/brettinternet/bump/internal/config"
)

func TestRunCommand(t *testing.T) {
	root := sandbox(t)
	manifest := `jobs:
  tests:
    description: Run the test suite
    run: 'echo child-out; echo child-err >&2; exit 7'
  safe:
    run: 'echo success'
  disabled:
    enabled: false
    run: 'touch SHOULD_NOT_EXIST'
`
	if err := os.WriteFile(filepath.Join(root, "bump.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name           string
		args           []string
		code           int
		stdout, stderr string
		json           bool
	}{
		{"bare", []string{"run"}, 0, "Next: bump run safe", "", false},
		{"bare json", []string{"run", "--json"}, 0, "description", "", true},
		{"suggestion", []string{"--json", "run", "tset"}, 1, `"code":"not_found"`, "", true},
		{"disabled", []string{"run", "disabled", "--json"}, 1, `"code":"disabled"`, "", true},
		{"extra argument", []string{"run", "tests", "extra", "--json"}, 2, `"code":"usage"`, "", true},
		{"json child exit", []string{"run", "--json", "tests"}, 7, `"exit_code":7`, "child-out", true},
		{"json after name", []string{"run", "tests", "--json"}, 7, `"status":"failed"`, "child-err", true},
		{"human child exit", []string{"run", "tests"}, 7, "child-out", "failed: exit 7", false},
		{"success", []string{"run", "safe"}, 0, "success", "succeeded", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := cli.Run(context.Background(), append([]string{"bump"}, tc.args...), config.BuildOpts{}, &out, &stderr)
			if code != tc.code || !strings.Contains(out.String(), tc.stdout) || !strings.Contains(stderr.String(), tc.stderr) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &stderr)
			}
			if tc.json {
				var result map[string]any
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatalf("impure JSON: %s: %v", &out, err)
				}
				if result["schema_version"] != float64(1) {
					t.Fatalf("%v", result)
				}
				if tc.name == "suggestion" && !strings.Contains(result["error"].(map[string]any)["hint"].(string), "tests") {
					t.Fatalf("%v", result)
				}
				if tc.name == "disabled" && !strings.Contains(result["error"].(map[string]any)["hint"].(string), "bump.yaml") {
					t.Fatalf("%v", result)
				}
				if strings.HasPrefix(tc.name, "json ") {
					if strings.Contains(out.String(), "child-") || !strings.Contains(stderr.String(), "child-out") || !strings.Contains(stderr.String(), "child-err") {
						t.Fatalf("stdout=%s stderr=%s", &out, &stderr)
					}
				}
			}
			if tc.name == "bare" && (!strings.Contains(out.String(), "Run the test suite") || strings.Contains(out.String(), "disabled") || stderr.Len() != 0) {
				t.Fatalf("stdout=%s stderr=%s", &out, &stderr)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatalf("disabled job ran: %v", err)
	}
}

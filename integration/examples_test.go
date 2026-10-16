package integration

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Users copy examples verbatim, and strict validation rejects any field or schedule
// that drifts from the implementation, so every shipped example must validate.
func TestExamplesValidate(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	if err := os.MkdirAll(filepath.Join(configHome, "noon"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	// Prompt jobs name profiles defined in the agent-profiles user config.
	profiles, err := os.ReadFile("../examples/ai/agent-profiles/user-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "noon", "config.yaml"), profiles, 0o600); err != nil {
		t.Fatal(err)
	}
	env := isolatedEnvironment(home, configHome, filepath.Join(root, "xdg-state"), filepath.Join(root, "state"))
	check := func(t *testing.T, args ...string) {
		t.Helper()
		command := exec.Command(noonBinary, append(args, "config", "check")...)
		command.Dir, command.Env = home, env
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("noon %s config check: %v\n%s", strings.Join(args, " "), err, output)
		}
	}

	examples, err := filepath.Abs("../examples")
	if err != nil {
		t.Fatal(err)
	}
	projects := 0
	err = filepath.WalkDir(examples, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch entry.Name() {
		case "noon.yaml":
			projects++
			check(t, "-C", filepath.Dir(path))
		case "user-config.yaml", "jobs.yaml":
			check(t, "--config", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if projects < 20 {
		t.Fatalf("found %d example projects; expected the examples tree", projects)
	}

	// The private override only makes sense layered over its shared noon.yaml.
	project := filepath.Join(root, "local-overrides")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	for source, target := range map[string]string{"noon.yaml": "noon.yaml", "dot-noon.yaml": ".noon.yaml"} {
		data, err := os.ReadFile(filepath.Join(examples, "basics", "local-overrides", source))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, target), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	check(t, "-C", project)
}

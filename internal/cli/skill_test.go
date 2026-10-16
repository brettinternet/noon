package cli_test

import (
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/skill"
	urfave "github.com/urfave/cli/v3"
)

// Flag renames must not leave shell-only agents with commands that fail.
func TestSkillFlags(t *testing.T) {
	root := cli.NewRootCommand(config.BuildOpts{}, io.Discard, io.Discard)
	examples := regexp.MustCompile("noon ([a-z]+)([^`\n]*)").FindAllStringSubmatch(skill.Content(), -1)
	flags := regexp.MustCompile(`--([a-z][a-z-]*)`)
	if len(examples) == 0 {
		t.Fatal("no Noon examples found")
	}
	for _, example := range examples {
		var command *urfave.Command
		for _, candidate := range root.Commands {
			if candidate.Name == example[1] || slices.Contains(candidate.Aliases, example[1]) {
				command = candidate
				break
			}
		}
		if command == nil {
			t.Errorf("unknown command in %q", example[0])
			continue
		}
		known := map[string]bool{}
		for _, group := range [][]urfave.Flag{root.Flags, command.Flags} {
			for _, flag := range group {
				for _, name := range flag.Names() {
					known[name] = true
				}
			}
		}
		// Stop at a shell pipeline; flags after it belong to another program.
		commandText, _, _ := strings.Cut(example[2], "|")
		for _, flag := range flags.FindAllStringSubmatch(commandText, -1) {
			if !known[flag[1]] {
				t.Errorf("unknown flag --%s in %q", flag[1], example[0])
			}
		}
	}
}

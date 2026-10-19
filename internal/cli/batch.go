package cli

import (
	"fmt"

	urfave "github.com/urfave/cli/v3"
)

// uniqueTargets preserves the user's order while avoiding repeated mutations.
func uniqueTargets(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

type targetFailure struct {
	Target string `json:"target"`
	Error  string `json:"error"`
}

// finishBatch emits one JSON object even when some irreversible operations failed.
func finishBatch(cmd *urfave.Command, output map[string]any, failures []targetFailure) error {
	if cmd.Bool("json") {
		output["errors"] = failures
		if err := emit(cmd, output); err != nil {
			return err
		}
	} else {
		for _, failure := range failures {
			fmt.Fprintf(cmd.ErrWriter, "%s: %s\n", humanText(failure.Target), humanText(failure.Error))
		}
	}
	if len(failures) > 0 {
		return runExit(1)
	}
	return nil
}

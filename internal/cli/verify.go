package cli

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/brettinternet/noon/internal/config"
)

func sameNames(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func editVerificationError(message string) error {
	return &Error{Code: "config_edit", Message: message, Hint: "Edit the file by hand"}
}

func verifyRawJobsUnchanged(before, after []byte, names []string, editedName string, cfg config.Config) error {
	for _, name := range names {
		if name == editedName {
			continue
		}
		oldJob, err := config.JobFromSource(before, name, cfg.Agents, cfg.DefaultAgent)
		if err != nil {
			return err
		}
		newJob, err := config.JobFromSource(after, name, cfg.Agents, cfg.DefaultAgent)
		if err != nil {
			return err
		}
		if oldJob.Digest != newJob.Digest || oldJob.Description != newJob.Description || oldJob.Enabled != newJob.Enabled {
			return fmt.Errorf("unrelated raw job %q changed during edit", name)
		}
	}
	return nil
}

func verifyConfigUnchanged(before, after config.Config, editedName string) error {
	if before.Project != after.Project || !reflect.DeepEqual(before.ConfigFile, after.ConfigFile) ||
		before.Timezone != after.Timezone || before.StateDir != after.StateDir ||
		before.DefaultAgent != after.DefaultAgent || !reflect.DeepEqual(before.Agents, after.Agents) {
		return fmt.Errorf("non-job configuration changed during edit")
	}
	for name, oldJob := range before.Jobs {
		if name == editedName {
			continue
		}
		newJob, ok := after.Jobs[name]
		if !ok || oldJob.Digest != newJob.Digest || oldJob.Description != newJob.Description || oldJob.Enabled != newJob.Enabled {
			return fmt.Errorf("unrelated job %q changed during edit", name)
		}
	}
	for name := range after.Jobs {
		if name == editedName {
			continue
		}
		if _, ok := before.Jobs[name]; !ok {
			return fmt.Errorf("unexpected job %q appeared during edit", name)
		}
	}
	return nil
}

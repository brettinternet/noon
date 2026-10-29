package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/cronimport"
	urfave "github.com/urfave/cli/v3"
)

func importers(_ context.Context, cmd *urfave.Command) error {
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"importers": []string{"crontab"}})
	}
	fmt.Fprintln(cmd.Writer, "Available importers:\n  crontab — user crontab entries")
	return nil
}

func importCrontab(_ context.Context, cmd *urfave.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		if cmd.Bool("json") {
			return emit(cmd, map[string]any{"usage": "noon import crontab FILE|-", "example": "crontab -l | noon import crontab -"})
		}
		fmt.Fprintln(cmd.Writer, "Usage: noon import crontab FILE|- [--write]")
		fmt.Fprintln(cmd.Writer, "Example: crontab -l | noon import crontab -")
		return nil
	}
	if len(args) != 1 {
		return usageError("import crontab requires one FILE or -", "Run noon import crontab FILE|- --help")
	}
	if cmd.Bool("write") && setting(cmd, "config") != nil {
		return &Error{Code: "explicit_config", Message: "cannot edit an explicit config file", Hint: "Edit that file directly", Usage: true}
	}

	var source io.Reader
	if args[0] == "-" {
		source = os.Stdin
	} else {
		file, err := os.Open(args[0])
		if err != nil {
			return configError(err)
		}
		defer file.Close()
		source = file
	}

	values, err := input(cmd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(values)
	if err != nil {
		return configError(err)
	}
	existingNames := make([]string, 0, len(cfg.Jobs))
	for name := range cfg.Jobs {
		existingNames = append(existingNames, name)
	}
	jobs, err := cronimport.ParseWithExisting(source, existingNames)
	if err != nil {
		return &Error{Code: "crontab_import", Message: err.Error(), Hint: "Fix the listed crontab lines; nothing was written."}
	}
	preview, err := cronimport.RenderYAML(jobs)
	if err != nil {
		return &Error{Code: "crontab_import", Message: err.Error()}
	}

	written := ""
	if cmd.Bool("write") && len(jobs) > 0 {
		written, err = writeImportedJobs(values, cfg, jobs)
		if err != nil {
			return err
		}
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"jobs": importJobOutput(jobs), "written": written != ""})
	}
	if _, err := cmd.Writer.Write(preview); err != nil {
		return err
	}
	if written != "" {
		fmt.Fprintf(cmd.Writer, "Wrote %d jobs to %s.\n", len(jobs), displayPath(cfg.Project, written))
	} else if cmd.Bool("write") {
		fmt.Fprintln(cmd.Writer, "No jobs to write.")
	}
	return nil
}

func importJobOutput(jobs []cronimport.Job) []map[string]any {
	output := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		output = append(output, map[string]any{
			"name": job.Name, "schedule": job.Schedule, "run": job.Run,
			"env_keys": job.EnvKeys(), "line": job.Line,
		})
	}
	return output
}

// writeImportedJobs returns the private file it wrote, or "" when there was nothing to write.
func writeImportedJobs(values config.Input, before config.Config, jobs []cronimport.Job) (string, error) {
	if len(jobs) == 0 {
		return "", nil
	}
	userProject := config.IsUserProject(before.Project, values.UserConfigDir)
	var err error
	file := ""
	if userProject {
		file = filepath.Join(before.Project, "config.yaml")
	} else {
		file, err = privateFile(before.Project)
		if err != nil {
			return "", err
		}
	}
	if err := refuseSymlink(file); err != nil {
		return "", err
	}
	source, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", configError(err)
	}
	oldNames, err := config.JobNames(source)
	if err != nil {
		return "", &Error{Code: "config_edit", Message: err.Error(), Hint: "Edit the file by hand"}
	}
	after := source
	for _, job := range jobs {
		after, err = cronimport.AddJobToYAML(after, job)
		if err != nil {
			return "", &Error{Code: "config_edit", Message: err.Error(), Hint: "Edit the file by hand"}
		}
	}
	wantNames := append(append([]string(nil), oldNames...), importedNames(jobs)...)
	newNames, err := config.JobNames(after)
	if err != nil {
		return "", &Error{Code: "config_edit", Message: err.Error(), Hint: "Edit the file by hand"}
	}
	if !sameNames(newNames, wantNames) {
		return "", editVerificationError("edited job names do not match the requested import")
	}
	if err := verifyRawJobsUnchanged(source, after, oldNames, "", before, true); err != nil {
		return "", editVerificationError(err.Error())
	}
	candidate, err := config.LoadWithFileOverride(values, file, after)
	if err != nil {
		return "", &Error{Code: "config_edit", Message: err.Error(), Hint: "Edit the file by hand"}
	}
	if err := verifyConfigUnchangedAfterAdding(before, candidate, importedNames(jobs)); err != nil {
		return "", editVerificationError(err.Error())
	}
	if userProject {
		if err := ensureUserConfigDir(before.Project); err != nil {
			return "", err
		}
		if err := atomicWrite(file, after, 0600); err != nil {
			return "", err
		}
	} else if err := atomicWrite(file, after, 0); err != nil {
		return "", err
	}
	return file, nil
}

func importedNames(jobs []cronimport.Job) []string {
	names := make([]string, len(jobs))
	for i, job := range jobs {
		names[i] = job.Name
	}
	return names
}

func verifyConfigUnchangedAfterAdding(before, after config.Config, addedNames []string) error {
	if before.Project != after.Project || !reflect.DeepEqual(before.ConfigFile, after.ConfigFile) ||
		before.Timezone != after.Timezone || before.StateDir != after.StateDir ||
		before.DefaultAgent != after.DefaultAgent || !reflect.DeepEqual(before.Agents, after.Agents) {
		return fmt.Errorf("non-job configuration changed during import")
	}
	added := make(map[string]struct{}, len(addedNames))
	for _, name := range addedNames {
		added[name] = struct{}{}
	}
	for name, previous := range before.Jobs {
		current, exists := after.Jobs[name]
		if !exists || previous.Digest != current.Digest || previous.Description != current.Description || previous.Enabled != current.Enabled {
			return fmt.Errorf("unrelated job %q changed during import", name)
		}
	}
	for name := range after.Jobs {
		if _, existed := before.Jobs[name]; existed {
			continue
		}
		if _, expected := added[name]; !expected {
			return fmt.Errorf("unexpected job %q appeared during import", name)
		}
	}
	for name := range added {
		if _, exists := after.Jobs[name]; !exists {
			return fmt.Errorf("imported job %q is missing", name)
		}
	}
	return nil
}

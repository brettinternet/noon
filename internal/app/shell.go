package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

// ShellEntry describes a directory transition in one shell, not a durable key.
// PreviousProject is an opaque canonical-path token returned by EnterProject.
type ShellEntry struct {
	Shell, Job, PreviousProject, PreviousDirectory, Directory string
	RecordOnly                                                bool
}

// EnterProject discovers the actual directory, then uses the enabled checkout's
// recorded configuration and the normal queue authorization path. It never
// enables projects or creates state for an ordinary no-op. The token is returned
// even on a queue failure so a prompt does not retry the same entry indefinitely.
func EnterProject(input config.Input, entry ShellEntry, now time.Time) (string, error) {
	input.CWD, input.Project, input.File, input.Timezone = entry.Directory, nil, nil, nil
	project, err := config.ProjectDir(input)
	if err != nil {
		return "", err
	}
	// Encoding avoids newline/word-splitting ambiguity in shell substitutions.
	token := base64.RawURLEncoding.EncodeToString([]byte(project))
	if entry.RecordOnly || token == entry.PreviousProject {
		return token, nil
	}
	stateDir, err := config.StateDirectory(input)
	if err != nil {
		return token, err
	}
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return token, nil
	}
	if err != nil {
		return token, err
	}
	approval, err := state.ApprovalForProject(project)
	closeErr := state.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return token, err
	}
	if !approval.Enabled {
		return token, nil
	}
	input.Project = &project
	if approval.ConfigFile.Valid {
		input.File = &approval.ConfigFile.String
	}
	cfg, err := config.Load(input)
	if err != nil {
		return token, err
	}
	// Like serve, keep using the store in which the checkout was enabled even
	// if the recorded explicit configuration names another state location.
	cfg.StateDir = stateDir
	if _, exists := cfg.Jobs[entry.Job]; !exists {
		return token, nil
	}
	data, err := json.Marshal(map[string]string{
		"event": "project_entry", "shell": entry.Shell,
		"previous_directory": entry.PreviousDirectory, "directory": entry.Directory,
		"project": project,
	})
	if err != nil {
		return token, err
	}
	_, err = QueueRun(cfg, entry.Job, "", now, now, nil, true, "shell:"+entry.Shell,
		QueueOptions{EventData: data, EventSource: "cli", RequireUnpaused: true})
	return token, err
}

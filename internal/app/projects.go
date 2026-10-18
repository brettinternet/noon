package app

import (
	"errors"
	"path/filepath"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

type ProjectSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type ProjectsView struct {
	SchemaVersion int              `json:"schema_version"`
	Projects      []ProjectSummary `json:"projects"`
}

// ListProjects returns the enabled projects in the shared scheduler state.
func ListProjects(stateDir string) (ProjectsView, error) {
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return ProjectsView{SchemaVersion: 1, Projects: []ProjectSummary{}}, nil
	}
	if err != nil {
		return ProjectsView{}, StateError(err)
	}
	defer state.Close()
	records, err := state.ProjectRecords()
	if err != nil {
		return ProjectsView{}, StateError(err)
	}
	projects := make([]ProjectSummary, 0, len(records))
	for _, record := range records {
		if record.Enabled {
			projects = append(projects, ProjectSummary{ID: record.ID, Name: filepath.Base(record.Path), Path: record.Path})
		}
	}
	return ProjectsView{SchemaVersion: 1, Projects: projects}, nil
}

// EnabledProjectInput resolves an enabled project ID using the config selection
// recorded by noon enable, never an arbitrary filesystem path.
func EnabledProjectInput(input config.Input, stateDir string, id int64) (config.Input, error) {
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return config.Input{}, projectNotFound()
	}
	if err != nil {
		return config.Input{}, StateError(err)
	}
	defer state.Close()
	records, err := state.ProjectRecords()
	if err != nil {
		return config.Input{}, StateError(err)
	}
	for _, record := range records {
		if record.ID != id || !record.Enabled {
			continue
		}
		return inputForProject(input, stateDir, record), nil
	}
	return config.Input{}, projectNotFound()
}

// EnabledProjectInputByPath resolves an absolute canonical path to an enabled
// project for HTTP MCP tools.
func EnabledProjectInputByPath(input config.Input, stateDir, path string) (config.Input, error) {
	if !filepath.IsAbs(path) {
		return config.Input{}, UsageError("project must be an absolute path", "Pass an absolute checkout path")
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return config.Input{}, projectNotFound()
	}
	projects, err := ListProjects(stateDir)
	if err != nil {
		return config.Input{}, err
	}
	for _, project := range projects.Projects {
		if project.Path == canonical {
			return EnabledProjectInput(input, stateDir, project.ID)
		}
	}
	return config.Input{}, projectNotFound()
}

// EnabledRunProject confirms that a globally addressed run belongs to an
// enabled project. Disabled and unknown projects intentionally share a 404.
func EnabledRunProject(stateDir string, runID int64) (ProjectSummary, error) {
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		if errors.Is(err, store.ErrNoDatabase) {
			return ProjectSummary{}, RunNotFoundError(runID)
		}
		return ProjectSummary{}, StateError(err)
	}
	defer state.Close()
	run, err := state.RunByID(runID)
	if err != nil {
		if errors.Is(err, store.ErrNoDatabase) || IsMissingRun(err) {
			return ProjectSummary{}, RunNotFoundError(runID)
		}
		return ProjectSummary{}, StateError(err)
	}
	records, err := state.ProjectRecords()
	if err != nil {
		return ProjectSummary{}, StateError(err)
	}
	for _, record := range records {
		if record.ID == run.ProjectID && record.Enabled {
			return ProjectSummary{ID: record.ID, Name: filepath.Base(record.Path), Path: record.Path}, nil
		}
	}
	return ProjectSummary{}, RunNotFoundError(runID)
}

func inputForProject(input config.Input, stateDir string, record store.ProjectRecord) config.Input {
	path := record.Path
	input.Project, input.User = &path, false
	input.StateDir = &stateDir
	input.File = nil
	if record.ConfigFile.Valid {
		file := record.ConfigFile.String
		input.File = &file
	}
	return input
}

func projectNotFound() *Error {
	return &Error{Code: "not_found", Message: "project not found"}
}

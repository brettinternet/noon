package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/service"
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

// ProjectStatus summarizes one enabled project for noon projects.
type ProjectStatus struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	State       string     `json:"state"`
	ConfigError string     `json:"config_error,omitempty"`
	Failing     []string   `json:"failing,omitempty"`
	NextAt      *time.Time `json:"next_at,omitempty"`
	// configFile is the --config FILE recorded at enable time, if any.
	configFile string
}

// ProjectStatusView holds every enabled project and one suggested command.
// NextProject, when set, is the path Next must target, and NextConfig the config
// file it was enabled with; adapters render them.
type ProjectStatusView struct {
	Projects    []ProjectStatus
	Next        string
	NextProject string
	NextConfig  string
}

// ProjectStatuses reads every enabled project's dashboard without creating
// state. A missing checkout is reported without loading its config.
func ProjectStatuses(input config.Input, now time.Time) (ProjectStatusView, error) {
	stateDir, err := config.StateDirectory(input)
	if err != nil {
		return ProjectStatusView{}, ConfigError(err)
	}
	list, err := ListProjects(stateDir)
	if err != nil {
		return ProjectStatusView{}, err
	}
	sort.Slice(list.Projects, func(i, j int) bool { return list.Projects[i].Path < list.Projects[j].Path })
	result := ProjectStatusView{Projects: make([]ProjectStatus, 0, len(list.Projects))}
	offline := false
	for _, project := range list.Projects {
		row := ProjectStatus{ID: project.ID, Name: project.Name, Path: project.Path, State: "ok"}
		if config.IsUserProject(project.Path, input.UserConfigDir) {
			row.Name = "user"
		}
		if _, err := os.Stat(project.Path); errors.Is(err, fs.ErrNotExist) {
			row.State = "missing"
			result.Projects = append(result.Projects, row)
			continue
		}
		values, err := EnabledProjectInput(input, stateDir, project.ID)
		if err != nil {
			return ProjectStatusView{}, err
		}
		if values.File != nil {
			row.configFile = *values.File
		}
		view, err := Dashboard(values, now)
		var configuration *DashboardConfigError
		if errors.As(err, &configuration) {
			row.State, row.ConfigError = "config_error", err.Error()
			result.Projects = append(result.Projects, row)
			continue
		}
		if err != nil {
			return ProjectStatusView{}, StateError(err)
		}
		row.Name = view.Project.Name
		offline = offline || view.Scheduler.State == "offline"
		for _, job := range view.Jobs {
			if job.Failing() {
				row.Failing = append(row.Failing, job.Name)
			}
			// The noon list rules: only enabled, approved, active jobs have a next run.
			if job.NextAt != nil && job.Enabled && job.PlatformSupported && !job.Paused && !job.Ended && job.Approval != "needs_approval" && (row.NextAt == nil || job.NextAt.Before(*row.NextAt)) {
				row.NextAt = job.NextAt
			}
		}
		switch {
		case view.Project.ConfigError != "":
			row.State, row.ConfigError, row.Failing, row.NextAt = "config_error", view.Project.ConfigError, nil, nil
		case len(row.Failing) > 0:
			row.State = "failing"
		}
		result.Projects = append(result.Projects, row)
	}
	first := func(state string) *ProjectStatus {
		for i := range result.Projects {
			if result.Projects[i].State == state {
				return &result.Projects[i]
			}
		}
		return nil
	}
	if project := first("missing"); project != nil {
		result.Next, result.NextProject = "noon disable", project.Path
	} else if project := first("config_error"); project != nil {
		result.Next, result.NextProject, result.NextConfig = "noon config check", project.Path, project.configFile
	} else if offline {
		options, err := service.CurrentConfig(stateDir)
		if err != nil {
			return ProjectStatusView{}, err
		}
		_, installed, err := service.IsInstalled(options)
		if err != nil && !service.IsUnsupported(err) {
			return ProjectStatusView{}, err
		}
		result.Next = "noon service install"
		if installed || err != nil {
			result.Next = "noon serve"
		}
	} else if project := first("failing"); project != nil {
		result.Next, result.NextProject, result.NextConfig = "noon logs "+project.Failing[0]+" -s failed", project.Path, project.configFile
	}
	return result, nil
}

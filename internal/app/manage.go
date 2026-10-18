package app

import (
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
	"time"
)

func EnableAddedJob(cfg config.Config, job config.Job, now time.Time) error {
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return err
	}
	defer state.Close()
	var explicit *string
	if cfg.ConfigFile != nil {
		explicit = cfg.ConfigFile
	}
	return state.EnableAddedJob(cfg.Project, explicit, job.Name, job.Digest, job.Shared, now)
}
func SetPaused(stateDir, project, name string, paused bool, now time.Time) error {
	_, err := SetJobsPaused(stateDir, project, []string{name}, paused, now)
	return err
}

func SetJobsPaused(stateDir, project string, names []string, paused bool, now time.Time) (int, error) {
	if len(names) == 0 {
		return 0, nil
	}
	state, err := store.Open(stateDir)
	if err != nil {
		return 0, err
	}
	defer state.Close()
	if _, err := state.RegisterProject(project, now); err != nil {
		return 0, err
	}
	return state.SetJobsPaused(project, names, paused, now)
}

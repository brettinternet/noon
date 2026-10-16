package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MaxEventBytes = 256 << 10

type eventFile struct {
	SchemaVersion int             `json:"schema_version"`
	Source        string          `json:"source"`
	ReceivedAt    string          `json:"received_at"`
	Data          json.RawMessage `json:"data"`
}

func (s *Store) writeEvent(runID int64, source string, receivedAt time.Time, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	if len(data) > MaxEventBytes || !json.Valid(data) {
		return "", errors.New("event data must be valid JSON of at most 256 KiB")
	}
	switch source {
	case "cli", "mcp", "webhook":
	default:
		return "", fmt.Errorf("invalid event source %q", source)
	}
	encoded, err := json.Marshal(eventFile{SchemaVersion: 1, Source: source, ReceivedAt: FormatTime(receivedAt), Data: bytes.TrimSpace(data)})
	if err != nil {
		return "", fmt.Errorf("encode run event: %w", err)
	}
	dir := filepath.Join(s.dir, "events")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create event directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("secure event directory: %w", err)
	}
	relative := filepath.ToSlash(filepath.Join("events", fmt.Sprintf("%d.json", runID)))
	path := filepath.Join(s.dir, filepath.FromSlash(relative))
	temporary, err := os.CreateTemp(dir, ".event-*")
	if err != nil {
		return "", fmt.Errorf("create temporary event file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return "", fmt.Errorf("secure temporary event file: %w", err)
	}
	written, err := temporary.Write(encoded)
	if err == nil && written != len(encoded) {
		err = fmt.Errorf("short event file write")
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("write event file: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("publish event file: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("remove temporary event file: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("sync event directory: %w", err)
	}
	return relative, nil
}

func (s *Store) removeEvent(relative string) error {
	return removeEventFile(s.dir, relative)
}

func removeEventFile(stateDir, relative string) error {
	if !validEventPath(relative) {
		return fmt.Errorf("unsafe event path %q", relative)
	}
	path := filepath.Join(stateDir, filepath.FromSlash(relative))
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old run event: %w", err)
	}
	return nil
}

func (s *Store) EventFilePath(run Run) (string, bool) {
	if !run.EventPath.Valid || !validEventPath(run.EventPath.String) {
		return "", false
	}
	return filepath.Join(s.dir, filepath.FromSlash(run.EventPath.String)), true
}

func validEventPath(relative string) bool {
	return !filepath.IsAbs(relative) && filepath.Clean(filepath.FromSlash(relative)) == filepath.FromSlash(relative) && strings.HasPrefix(filepath.FromSlash(relative), "events"+string(filepath.Separator)) && !strings.HasPrefix(filepath.FromSlash(relative), ".."+string(filepath.Separator))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

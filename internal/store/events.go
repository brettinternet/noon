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

const (
	MaxEventBytes       = 256 << 10
	MaxWebhookBodyBytes = 1 << 20
	// Allow sixfold JSON escaping for the body and HTTP headers, plus envelope overhead.
	maxWebhookEventBytes = 6*(MaxWebhookBodyBytes+(20<<10)) + (1 << 10)
)

type eventFile struct {
	SchemaVersion int                `json:"schema_version"`
	Source        string             `json:"source"`
	ReceivedAt    string             `json:"received_at"`
	Data          json.RawMessage    `json:"data"`
	Headers       *map[string]string `json:"headers,omitempty"`
}

func (s *Store) writeEvent(runID int64, source string, receivedAt time.Time, data []byte, headerSets ...map[string]string) (string, error) {
	if len(data) == 0 && source != "webhook" {
		return "", nil
	}
	var headers map[string]string
	switch source {
	case "cli", "mcp":
		if len(data) > MaxEventBytes || !json.Valid(data) {
			return "", errors.New("event data must be valid JSON of at most 256 KiB")
		}
	case "webhook":
		if len(data) > MaxWebhookBodyBytes {
			return "", errors.New("webhook body exceeds 1 MiB")
		}
		if !json.Valid(data) {
			var err error
			data, err = json.Marshal(string(data))
			if err != nil {
				return "", fmt.Errorf("encode webhook body: %w", err)
			}
		}
		if len(headerSets) > 0 {
			headers = safeWebhookHeaders(headerSets[0])
		}
	default:
		return "", fmt.Errorf("invalid event source %q", source)
	}
	var savedHeaders *map[string]string
	if headers != nil {
		savedHeaders = &headers
	}
	encoded, err := json.Marshal(eventFile{SchemaVersion: 1, Source: source, ReceivedAt: FormatTime(receivedAt), Data: bytes.TrimSpace(data), Headers: savedHeaders})
	if err != nil {
		return "", fmt.Errorf("encode run event: %w", err)
	}
	if source == "webhook" && len(encoded) > maxWebhookEventBytes {
		return "", errors.New("webhook event exceeds serialized size limit")
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

func safeWebhookHeaders(headers map[string]string) map[string]string {
	allowed := map[string]bool{
		"content-type":      true,
		"user-agent":        true,
		"x-github-event":    true,
		"x-github-delivery": true,
		"x-noon-delivery":   true,
	}
	result := make(map[string]string)
	for key, value := range headers {
		key = strings.ToLower(key)
		if allowed[key] {
			result[key] = value
		}
	}
	return result
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

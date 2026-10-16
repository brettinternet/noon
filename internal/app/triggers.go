package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/brettinternet/noon/internal/store"
)

const maxTriggerDepth = 3

func ValidateEventData(data []byte) error {
	if len(data) > store.MaxEventBytes {
		return UsageError("--data must be at most 256 KiB", "Use a smaller JSON value")
	}
	if !json.Valid(data) {
		return UsageError("--data must contain valid JSON", "Pass a JSON value, @FILE, or - for stdin")
	}
	return nil
}

// RequestDepth returns the depth carried by the requesting process. Invalid
// and negative values start at zero rather than granting extra recursion.
func RequestDepth(environment []string) int {
	depth := 0
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key != "NOON_DEPTH" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			depth = 0
		} else {
			depth = parsed
		}
	}
	return depth
}

// CheckRecursion prevents unbounded queued chains and same-job self triggers.
func CheckRecursion(project, job string, environment []string) error {
	values := make(map[string]string)
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	depth := RequestDepth(environment)
	if depth >= maxTriggerDepth {
		return &Error{Code: "recursion", Message: fmt.Sprintf("recursion guard refused request at NOON_DEPTH=%d", depth)}
	}
	if values["NOON_JOB"] == job && values["NOON_PROJECT"] != "" && canonicalProject(values["NOON_PROJECT"]) == canonicalProject(project) {
		return &Error{Code: "recursion", Message: fmt.Sprintf("recursion guard refused %s triggering itself", job)}
	}
	return nil
}

func canonicalProject(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return canonical
	}
	return filepath.Clean(absolute)
}

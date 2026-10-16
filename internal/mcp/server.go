// Package mcp adapts shared Noon operations to the MCP stdio protocol.
package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultLogBytes = 16_384

type projectArgs struct {
	Project *string `json:"project,omitempty" jsonschema:"absolute path to another checkout"`
}

type runJobArgs struct {
	Name    string  `json:"name" jsonschema:"configured job name"`
	Project *string `json:"project,omitempty" jsonschema:"absolute path to another checkout"`
	Key     *string `json:"key,omitempty" jsonschema:"idempotency key"`
	In      *string `json:"in,omitempty" jsonschema:"queue after a duration, such as 2h"`
	At      *string `json:"at,omitempty" jsonschema:"queue at HH:MM, local date/time, or RFC 3339"`
	Data    any     `json:"data,omitempty" jsonschema:"JSON event payload (up to 256 KiB)"`
}

type listRunsArgs struct {
	Project *string  `json:"project,omitempty" jsonschema:"absolute path to another checkout"`
	Job     *string  `json:"job,omitempty" jsonschema:"filter runs to a configured or retained job"`
	Status  []string `json:"status,omitempty" jsonschema:"filter by run status: queued, running, succeeded, failed, timed_out, cancelled, interrupted, skipped, missed, expired"`
	Limit   *int     `json:"limit,omitempty" jsonschema:"maximum 50; defaults to 20"`
}

type getRunArgs struct {
	RunID       *int64   `json:"run_id,omitempty" jsonschema:"globally unique run ID; provide this or job"`
	Job         *string  `json:"job,omitempty" jsonschema:"select the newest started matching run by default; provide this or run_id"`
	Project     *string  `json:"project,omitempty" jsonschema:"absolute project path used with job"`
	Status      []string `json:"status,omitempty" jsonschema:"when selecting by job, filter by status: queued, running, succeeded, failed, timed_out, cancelled, interrupted, skipped, missed, expired"`
	WaitSeconds *int     `json:"wait_seconds,omitempty" jsonschema:"wait up to 60 seconds for the run to finish"`
	LogBytes    *int     `json:"log_bytes,omitempty" jsonschema:"maximum 65536 bytes; defaults to 16384 and keeps whole lines"`
	After       *int64   `json:"after,omitempty" jsonschema:"start at this byte offset; pass the previous next value to page forward"`
	Tail        *int     `json:"tail,omitempty" jsonschema:"keep the last 1 to 10000 selected log lines"`
	Match       *string  `json:"match,omitempty" jsonschema:"case-sensitive Go RE2 regular expression matched against each line"`
	Context     *int     `json:"context,omitempty" jsonschema:"include 0 to 50 lines around each match; requires match"`
}

type cancelRunArgs struct {
	RunID int64 `json:"run_id" jsonschema:"globally unique run ID"`
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

type errorOutput struct {
	SchemaVersion int           `json:"schema_version"`
	Error         errorEnvelope `json:"error"`
}

// NewServer builds the six-tool MCP server over shared app operations.
func NewServer(input config.Input, version string) *protocol.Server {
	server := protocol.NewServer(&protocol.Implementation{Name: "noon", Version: version}, nil)
	readOnly := &protocol.ToolAnnotations{ReadOnlyHint: true}
	destructive := true

	protocol.AddTool(server, &protocol.Tool{
		Name: "status", Description: "Show the project dashboard without running anything.", Annotations: readOnly,
	}, func(_ context.Context, _ *protocol.CallToolRequest, args projectArgs) (*protocol.CallToolResult, any, error) {
		values, err := projectInput(input, args.Project)
		if err != nil {
			return toolFailure(err)
		}
		view, err := app.Dashboard(values, time.Now())
		if err != nil {
			var configErr *app.DashboardConfigError
			if errors.As(err, &configErr) {
				err = app.ConfigError(err)
			} else {
				err = app.StateError(err)
			}
			return toolFailure(err)
		}
		return toolSuccess(fmt.Sprintf("Status for %s.", view.Project.Name), view)
	})

	protocol.AddTool(server, &protocol.Tool{
		Name: "list_jobs", Description: "List validated jobs and their status without executing them.", Annotations: readOnly,
	}, func(_ context.Context, _ *protocol.CallToolRequest, args projectArgs) (*protocol.CallToolResult, any, error) {
		values, err := projectInput(input, args.Project)
		if err != nil {
			return toolFailure(err)
		}
		view, err := app.ListJobs(values, time.Now())
		if err != nil {
			return toolFailure(err)
		}
		return toolSuccess(fmt.Sprintf("Listed %d jobs.", len(view.Jobs)), view)
	})

	protocol.AddTool(server, &protocol.Tool{
		Name: "run_job", Description: "Queue a configured job; shared jobs must already be approved.",
	}, func(_ context.Context, req *protocol.CallToolRequest, args runJobArgs) (*protocol.CallToolResult, any, error) {
		values, err := projectInput(input, args.Project)
		if err != nil {
			return toolFailure(err)
		}
		if args.In != nil && args.At != nil {
			return toolFailure(app.UsageError("--detach, --in, and --at are mutually exclusive", "Choose one queued run time"))
		}
		eventData, hasEventData, err := mcpEventData(req)
		if err != nil {
			return toolFailure(err)
		}
		if hasEventData {
			if err := app.ValidateEventData(eventData); err != nil {
				return toolFailure(err)
			}
		}
		key := ""
		if args.Key != nil {
			key = *args.Key
			if !app.ValidIdempotencyKey(key) {
				return toolFailure(app.UsageError("--key must be 1–200 characters of [A-Za-z0-9._:-]", "Choose a key using letters, digits, dot, underscore, colon, or hyphen"))
			}
		}
		cfg, err := config.Load(values)
		if err != nil {
			return toolFailure(app.ConfigError(err))
		}
		now := time.Now()
		location, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			return toolFailure(app.ConfigError(err))
		}
		mode, inValue, atValue := "detach", "", ""
		if args.In != nil {
			mode, inValue = "in", *args.In
		}
		if args.At != nil {
			mode, atValue = "at", *args.At
		}
		dueAt, err := app.QueueDueAt(mode, inValue, atValue, now, location)
		if err != nil {
			return toolFailure(app.UsageError(err.Error(), "Use --in 2h or --at '2026-10-16 09:00'"))
		}
		result, err := app.QueueRun(cfg, args.Name, key, dueAt, now, time.Now, mode == "detach", requestedBy(req), app.QueueOptions{EventData: eventData, EventSource: "mcp"})
		if err != nil {
			return toolFailure(queueError(err))
		}
		summary := fmt.Sprintf("Queued %s as run %d.", result.Run.Job, result.Run.ID)
		if result.Reason == "cooldown" {
			summary = fmt.Sprintf("%s is cooling down until %s (run %d).", result.Run.Job, result.CooldownUntil, result.Run.ID)
		} else if !result.Created {
			summary = fmt.Sprintf("Run %d was already requested.", result.Run.ID)
		}
		return toolSuccess(summary, result)
	})

	protocol.AddTool(server, &protocol.Tool{
		Name: "list_runs", Description: "List recent runs for a project, optionally filtered by job and status.", Annotations: readOnly,
	}, func(_ context.Context, _ *protocol.CallToolRequest, args listRunsArgs) (*protocol.CallToolResult, any, error) {
		values, err := projectInput(input, args.Project)
		if err != nil {
			return toolFailure(err)
		}
		limit := 20
		if args.Limit != nil {
			limit = *args.Limit
		}
		view, err := app.ListRuns(values, args.Job, args.Status, limit)
		if err != nil {
			return toolFailure(err)
		}
		return toolSuccess(fmt.Sprintf("Listed %d runs.", len(view.Runs)), view)
	})

	protocol.AddTool(server, &protocol.Tool{
		Name: "get_run", Description: "Get a run by run_id or select the newest matching run by job, optionally filtering by project and status. Returns a bounded, line-aligned log selection; use next as after to page forward, or match and context to search, and optionally wait for the run to finish.", Annotations: readOnly,
	}, func(ctx context.Context, _ *protocol.CallToolRequest, args getRunArgs) (*protocol.CallToolResult, any, error) {
		if (args.RunID == nil) == (args.Job == nil) {
			return toolFailure(app.UsageError("get_run requires exactly one of run_id or job", "Provide run_id or job, but not both"))
		}
		if args.Job != nil && *args.Job == "" {
			return toolFailure(app.MatchingJobRunNotFoundError(*args.Job, args.Status))
		}
		if err := app.ValidateRunStatuses(args.Status); err != nil {
			return toolFailure(err)
		}
		if args.RunID != nil && (args.Project != nil || len(args.Status) > 0) {
			return toolFailure(app.UsageError("project and status can only be used with job selection", "Use get_run with job, or omit project and status when using run_id"))
		}
		values := input
		if args.Job != nil {
			var err error
			values, err = projectInput(input, args.Project)
			if err != nil {
				return toolFailure(err)
			}
		}
		stateDir, err := stateDirectory(values)
		if err != nil {
			return toolFailure(err)
		}
		waitSeconds := 0
		if args.WaitSeconds != nil {
			waitSeconds = *args.WaitSeconds
		}
		logBytes := defaultLogBytes
		if args.LogBytes != nil {
			logBytes = *args.LogBytes
		}
		query := app.LogQuery{After: args.After, Tail: args.Tail, Match: args.Match, Context: args.Context, LimitBytes: &logBytes, RepairUTF8: true}
		var view app.RunDetail
		if args.RunID != nil {
			view, err = app.GetRun(ctx, stateDir, *args.RunID, waitSeconds, query)
		} else {
			cfg, configErr := config.Load(values)
			if configErr != nil {
				return toolFailure(app.ConfigError(configErr))
			}
			run, found, selectErr := app.LatestRun(stateDir, cfg.Project, *args.Job, args.Status)
			if selectErr != nil {
				return toolFailure(app.StateError(selectErr))
			}
			if !found {
				return toolFailure(app.MatchingJobRunNotFoundError(*args.Job, args.Status))
			}
			view, err = app.GetRun(ctx, stateDir, run.ID, waitSeconds, query)
		}
		if err != nil {
			return toolFailure(err)
		}
		return toolSuccess(fmt.Sprintf("Run %d is %s.", view.Run.ID, view.Run.Status), view)
	})

	protocol.AddTool(server, &protocol.Tool{
		Name: "cancel_run", Description: "Cancel a queued or running run by its globally unique ID.",
		Annotations: &protocol.ToolAnnotations{DestructiveHint: &destructive},
	}, func(_ context.Context, _ *protocol.CallToolRequest, args cancelRunArgs) (*protocol.CallToolResult, any, error) {
		stateDir, err := stateDirectory(input)
		if err != nil {
			return toolFailure(err)
		}
		result, err := app.Cancel(stateDir, "", &args.RunID, "", time.Now())
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrNoDatabase) {
			return toolFailure(app.RunNotFoundError(args.RunID))
		}
		if err != nil {
			return toolFailure(app.StateError(err))
		}
		status := "updated"
		if len(result.Runs) > 0 {
			status = result.Runs[0].Status
		}
		return toolSuccess(fmt.Sprintf("Run %d is %s.", args.RunID, status), result)
	})

	return server
}

func projectInput(input config.Input, project *string) (config.Input, error) {
	if project == nil {
		return input, nil
	}
	if !filepath.IsAbs(*project) {
		return config.Input{}, app.UsageError("project must be an absolute path", "Use an absolute checkout path")
	}
	input.Project = project
	return input, nil
}

func stateDirectory(input config.Input) (string, error) {
	cfg, err := config.LoadForDisable(input)
	if err != nil {
		return "", app.ConfigError(err)
	}
	return cfg.StateDir, nil
}

func mcpEventData(req *protocol.CallToolRequest) ([]byte, bool, error) {
	var arguments map[string]json.RawMessage
	if err := json.Unmarshal(req.Params.Arguments, &arguments); err != nil {
		return nil, false, app.UsageError("run_job arguments must be a JSON object", "Pass a valid MCP tool argument object")
	}
	data, ok := arguments["data"]
	return data, ok, nil
}

func requestedBy(req *protocol.CallToolRequest) string {
	client := req.ClientInfo()
	if client == nil {
		return "mcp:unknown/unknown"
	}
	name, version := client.Name, client.Version
	if name == "" {
		name = "unknown"
	}
	if version == "" {
		version = "unknown"
	}
	return "mcp:" + name + "/" + version
}

func queueError(err error) error {
	var queueErr *app.QueueError
	if errors.As(err, &queueErr) {
		return &app.Error{Code: queueErr.Code, Message: queueErr.Message, Hint: queueErr.Hint, Usage: queueErr.Code == "usage"}
	}
	var operationError *app.Error
	if errors.As(err, &operationError) {
		return operationError
	}
	return app.StateError(err)
}

func toolSuccess(summary string, output any) (*protocol.CallToolResult, any, error) {
	return &protocol.CallToolResult{Content: []protocol.Content{&protocol.TextContent{Text: summary}}}, output, nil
}

func toolFailure(err error) (*protocol.CallToolResult, any, error) {
	var failure *app.Error
	if !errors.As(err, &failure) {
		var configErr *app.DashboardConfigError
		if errors.As(err, &configErr) {
			failure = app.ConfigError(configErr)
		} else {
			failure = app.StateError(err)
		}
	}
	output := errorOutput{SchemaVersion: 1, Error: errorEnvelope{Code: failure.Code, Message: failure.Message, Hint: failure.Hint}}
	text := "noon: " + failure.Message
	if failure.Hint != "" {
		text += "\nHint: " + failure.Hint
	}
	return &protocol.CallToolResult{IsError: true, Content: []protocol.Content{&protocol.TextContent{Text: text}}}, output, nil
}

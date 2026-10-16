package mcp_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	noonmcp "github.com/brettinternet/noon/internal/mcp"
	"github.com/brettinternet/noon/internal/scheduler"
	"github.com/brettinternet/noon/internal/store"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCP(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", root)
	userConfigDir := filepath.Join(root, "user")
	stateDir := filepath.Join(root, "state", "noon")
	t.Setenv("XDG_CONFIG_HOME", userConfigDir)
	t.Setenv("XDG_STATE_HOME", filepath.Dir(stateDir))
	input := config.Input{CWD: root, UserConfigDir: userConfigDir, StateDirDefault: stateDir}

	manifest := "jobs:\n  shared: {run: 'true'}\n  approved: {run: 'printf mcp-finished'}\n  queued-cancel: {run: 'true'}\n"
	writeManifest(t, root, manifest)
	cfg, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.Enable(cfg, time.Now()); err != nil {
		t.Fatalf("enable fixture project: %v", err)
	}
	writeManifest(t, root, strings.Replace(manifest, "shared: {run: 'true'}", "shared: {run: 'false'}", 1))

	ctx := context.Background()
	server := noonmcp.NewServer(input, "test")
	clientTransport, serverTransport := protocol.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := protocol.NewClient(&protocol.Implementation{Name: "mcp-test", Version: "1.2.3"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		if err := serverSession.Wait(); err != nil {
			t.Errorf("MCP server session: %v", err)
		}
	})

	tools, err := clientSession.ListTools(ctx, &protocol.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	toolNames := make([]string, 0, len(tools.Tools))
	byName := make(map[string]*protocol.Tool, len(tools.Tools))
	for _, tool := range tools.Tools {
		toolNames = append(toolNames, tool.Name)
		byName[tool.Name] = tool
	}
	slices.Sort(toolNames)
	wantTools := []string{"cancel_run", "get_run", "list_jobs", "list_runs", "run_job", "status"}
	if !reflect.DeepEqual(toolNames, wantTools) {
		t.Fatalf("tools/list names = %v, want %v", toolNames, wantTools)
	}
	var cliOut, cliErr bytes.Buffer
	if code := cli.Run(ctx, []string{"noon", "--json", "mcp"}, config.BuildOpts{}, &cliOut, &cliErr); code != 2 {
		t.Fatalf("mcp --json exit=%d stdout=%s stderr=%s", code, &cliOut, &cliErr)
	}
	var usage map[string]any
	if err := json.Unmarshal(cliOut.Bytes(), &usage); err != nil || usage["error"].(map[string]any)["code"] != "usage" {
		t.Fatalf("mcp --json did not produce the CLI usage error: %s err=%v", &cliOut, err)
	}
	for _, name := range []string{"status", "list_jobs", "list_runs", "get_run"} {
		if byName[name].Annotations == nil || !byName[name].Annotations.ReadOnlyHint {
			t.Errorf("%s missing readOnlyHint", name)
		}
	}
	if byName["cancel_run"].Annotations == nil || byName["cancel_run"].Annotations.DestructiveHint == nil || !*byName["cancel_run"].Annotations.DestructiveHint {
		t.Error("cancel_run missing destructiveHint")
	}

	listResult := callTool(t, ctx, clientSession, "list_jobs", map[string]any{})
	if listResult.IsError {
		t.Fatalf("list_jobs failed: %s", textContent(listResult))
	}
	var listOut, listErr bytes.Buffer
	if code := cli.Run(ctx, []string{"noon", "list", "--json"}, config.BuildOpts{}, &listOut, &listErr); code != 0 {
		t.Fatalf("noon list --json: exit=%d stdout=%s stderr=%s", code, &listOut, &listErr)
	}
	var wantList, gotList any
	if err := json.Unmarshal(listOut.Bytes(), &wantList); err != nil {
		t.Fatal(err)
	}
	gotList = decodeValue(t, listResult.StructuredContent)
	if !reflect.DeepEqual(gotList, wantList) {
		t.Fatalf("list_jobs differs from noon list --json:\n got: %s\nwant: %s", listResult.StructuredContent, listOut.Bytes())
	}

	statusResult := callTool(t, ctx, clientSession, "status", map[string]any{})
	if statusResult.IsError || decodeObject(t, statusResult.StructuredContent)["schema_version"] != float64(1) {
		t.Fatalf("status result: %s", textContent(statusResult))
	}
	if result := callTool(t, ctx, clientSession, "list_jobs", map[string]any{"project": "relative"}); !result.IsError || decodeObject(t, result.StructuredContent)["error"].(map[string]any)["code"] != "usage" {
		t.Fatalf("relative project path was accepted: %+v", result)
	}

	refused := callTool(t, ctx, clientSession, "run_job", map[string]any{"name": "shared"})
	if !refused.IsError {
		t.Fatalf("unapproved shared job succeeded: %+v", refused)
	}
	refusal := decodeObject(t, refused.StructuredContent)["error"].(map[string]any)
	if refusal["code"] != "needs_approval" || refusal["hint"] != "Run noon enable" {
		t.Fatalf("unapproved shared job error = %+v", refusal)
	}
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(input)
	if err != nil {
		_ = state.Close()
		t.Fatal(err)
	}
	unapproved, err := state.RunsForProject(cfg.Project, "shared", 50)
	_ = state.Close()
	if err != nil || len(unapproved) != 0 {
		t.Fatalf("rejected job request created a run: runs=%+v err=%v", unapproved, err)
	}

	queued := callTool(t, ctx, clientSession, "run_job", map[string]any{"name": "approved", "in": "1h"})
	if queued.IsError {
		t.Fatalf("approved job queue: %s", textContent(queued))
	}
	queuedValue := decodeObject(t, queued.StructuredContent)
	queuedRunID := int64(queuedValue["run"].(map[string]any)["id"].(float64))
	queuedDetail := callTool(t, ctx, clientSession, "get_run", map[string]any{"run_id": queuedRunID})
	if queuedDetail.IsError {
		t.Fatalf("get_run queued run: %s", textContent(queuedDetail))
	}
	queuedValue = decodeObject(t, queuedDetail.StructuredContent)
	queuedRun := queuedValue["run"].(map[string]any)
	if queuedRun["status"] != "queued" || queuedValue["log"] != "" || queuedRun["log"].(map[string]any)["available"] != false {
		t.Fatalf("get_run queued run returned unavailable-log error or wrong details: %+v", queuedValue)
	}
	missing := callTool(t, ctx, clientSession, "get_run", map[string]any{"run_id": int64(999999)})
	if !missing.IsError || decodeObject(t, missing.StructuredContent)["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("get_run did not preserve missing-run error: %+v", missing)
	}
	state, err = store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := state.RunByID(queuedRunID)
	_ = state.Close()
	if err != nil || stored.Status != "queued" || stored.RequestedBy.String != "mcp:mcp-test/1.2.3" {
		t.Fatalf("queued MCP run metadata: run=%+v err=%v", stored, err)
	}

	runs := callTool(t, ctx, clientSession, "list_runs", map[string]any{"job": "approved"})
	if runs.IsError {
		t.Fatalf("list_runs: %s", textContent(runs))
	}
	if got := len(decodeObject(t, runs.StructuredContent)["runs"].([]any)); got != 1 {
		t.Fatalf("list_runs returned %d records, want one", got)
	}

	immediate := callTool(t, ctx, clientSession, "run_job", map[string]any{"name": "approved"})
	if immediate.IsError {
		t.Fatalf("immediate job queue: %s", textContent(immediate))
	}
	immediateID := int64(decodeObject(t, immediate.StructuredContent)["run"].(map[string]any)["id"].(float64))
	boundedWait := callTool(t, ctx, clientSession, "get_run", map[string]any{"run_id": immediateID, "wait_seconds": 61})
	if !boundedWait.IsError || decodeObject(t, boundedWait.StructuredContent)["error"].(map[string]any)["code"] != "usage" {
		t.Fatalf("get_run accepted a wait over 60 seconds: %+v", boundedWait)
	}
	boundedLog := callTool(t, ctx, clientSession, "get_run", map[string]any{"run_id": immediateID, "log_bytes": 65537})
	if !boundedLog.IsError || decodeObject(t, boundedLog.StructuredContent)["error"].(map[string]any)["code"] != "usage" {
		t.Fatalf("get_run accepted a log tail over 65536 bytes: %+v", boundedLog)
	}

	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	schedulerDone := make(chan error, 1)
	go func() {
		schedulerDone <- scheduler.Serve(schedulerCtx, scheduler.ServeOptions{
			StateDir: stateDir, StateDirDefault: stateDir, UserConfigDir: userConfigDir,
			Version: "test", Environment: os.Environ(), Logger: io.Discard, IgnoreSignals: true,
		})
	}()
	defer func() {
		stopScheduler()
		select {
		case err := <-schedulerDone:
			if err != nil {
				t.Errorf("stop scheduler: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("scheduler did not stop")
		}
	}()

	detail := callTool(t, ctx, clientSession, "get_run", map[string]any{"run_id": immediateID, "wait_seconds": 5})
	if detail.IsError {
		t.Fatalf("get_run wait: %s", textContent(detail))
	}
	detailValue := decodeObject(t, detail.StructuredContent)
	if detailValue["run"].(map[string]any)["status"] != "succeeded" || !strings.Contains(detailValue["log"].(string), "mcp-finished") {
		t.Fatalf("get_run did not return the finished run and log: %+v", detailValue)
	}
	tail := callTool(t, ctx, clientSession, "get_run", map[string]any{"run_id": immediateID, "log_bytes": 1})
	if tail.IsError || decodeObject(t, tail.StructuredContent)["log"] != "d" {
		t.Fatalf("get_run did not enforce a one-byte log tail: %+v", tail)
	}
	state, err = store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	stored, err = state.RunByID(immediateID)
	_ = state.Close()
	if err != nil || !stored.LogPath.Valid {
		t.Fatalf("get_run fixture log is not retained before pruning: run=%+v err=%v", stored, err)
	}
	if err := os.Remove(filepath.Join(stateDir, stored.LogPath.String)); err != nil {
		t.Fatalf("prune fixture log: %v", err)
	}
	prunedDetail := callTool(t, ctx, clientSession, "get_run", map[string]any{"run_id": immediateID})
	if prunedDetail.IsError {
		t.Fatalf("get_run pruned run: %s", textContent(prunedDetail))
	}
	prunedValue := decodeObject(t, prunedDetail.StructuredContent)
	if prunedValue["run"].(map[string]any)["log"].(map[string]any)["available"] != false || prunedValue["log"] != "" {
		t.Fatalf("get_run returned a pruned log as available: %+v", prunedValue)
	}

	cancelQueued := callTool(t, ctx, clientSession, "run_job", map[string]any{"name": "queued-cancel", "in": "1h"})
	if cancelQueued.IsError {
		t.Fatalf("queue run to cancel: %s", textContent(cancelQueued))
	}
	cancelID := int64(decodeObject(t, cancelQueued.StructuredContent)["run"].(map[string]any)["id"].(float64))
	cancelResult := callTool(t, ctx, clientSession, "cancel_run", map[string]any{"run_id": cancelID})
	if cancelResult.IsError {
		t.Fatalf("cancel_run: %s", textContent(cancelResult))
	}
	cancelled := decodeObject(t, cancelResult.StructuredContent)["runs"].([]any)
	if len(cancelled) != 1 || cancelled[0].(map[string]any)["status"] != "cancelled" {
		t.Fatalf("queued run cancellation result: %+v", cancelled)
	}
	cancelledDetail := callTool(t, ctx, clientSession, "get_run", map[string]any{"run_id": cancelID})
	if cancelledDetail.IsError {
		t.Fatalf("get_run cancelled run: %s", textContent(cancelledDetail))
	}
	cancelledValue := decodeObject(t, cancelledDetail.StructuredContent)
	cancelledRun := cancelledValue["run"].(map[string]any)
	if cancelledRun["status"] != "cancelled" || cancelledValue["log"] != "" || cancelledRun["log"].(map[string]any)["available"] != false {
		t.Fatalf("get_run cancelled run returned unavailable-log error or wrong details: %+v", cancelledValue)
	}
}

func TestServeStdioCanonicalIDsAndEOFDrain(t *testing.T) {
	input, _, _ := stdioFixture(t)
	server := noonmcp.NewServer(input, "test")
	lines := []string{
		`{"jsonrpc":"2.0","id":"\u0061","method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"stdio-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		marshalProtocolLine(t, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}}),
	}
	output, elapsed, err := serveStdio(t, server, strings.Join(lines, "\n")+"\n", 2*time.Second)
	if err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("ServeStdio did not drain EOF promptly: %s", elapsed)
	}
	responses := protocolResponses(t, output)
	if len(responses) != 2 {
		t.Fatalf("got %d responses, want initialize and tools/list: %s", len(responses), output)
	}
	if responseForID(responses, "a") == nil {
		t.Fatalf("initialize response did not canonicalize escaped id: %s", output)
	}
	list := responseForID(responses, float64(2))
	if list == nil || len(list["result"].(map[string]any)["tools"].([]any)) != 6 {
		t.Fatalf("tools/list response was not drained: %s", output)
	}
}

func TestServeStdioConcurrentCancellationAndEOFDrain(t *testing.T) {
	input, _, runID := stdioFixture(t)
	server := noonmcp.NewServer(input, "test")
	lines := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"stdio-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		marshalProtocolLine(t, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "get_run", "arguments": map[string]any{"run_id": runID, "wait_seconds": 3}}}),
		marshalProtocolLine(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 2, "reason": "test cancellation"}}),
		marshalProtocolLine(t, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "cancel_run", "arguments": map[string]any{"run_id": runID}}}),
	}
	output, elapsed, err := serveStdio(t, server, strings.Join(lines, "\n")+"\n", 2*time.Second)
	if err != nil {
		t.Fatalf("ServeStdio concurrent requests: %v", err)
	}
	responses := protocolResponses(t, output)
	if len(responses) != 3 {
		t.Fatalf("got %d responses, want initialize, get_run, and cancel_run: %s", len(responses), output)
	}
	getRun := responseForID(responses, float64(2))
	cancelledGet := getRun != nil && getRun["error"] != nil
	if getRun != nil {
		if result, ok := getRun["result"].(map[string]any); ok {
			cancelledGet = cancelledGet || result["isError"] == true
		}
	}
	if !cancelledGet {
		t.Fatalf("notifications/cancelled did not interrupt the waiting get_run: %s", output)
	}
	cancel := responseForID(responses, float64(3))
	if cancel == nil {
		t.Fatalf("cancel_run response missing: %s", output)
	}
	cancelledRuns := cancel["result"].(map[string]any)["structuredContent"].(map[string]any)["runs"].([]any)
	if len(cancelledRuns) != 1 || cancelledRuns[0].(map[string]any)["status"] != "cancelled" {
		t.Fatalf("cancel_run did not cancel the waiting run: %+v", cancel["result"])
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("get_run blocked subsequent cancellation for %s", elapsed)
	}
}

func TestServeStdioContextShutdownCancelsPendingRequest(t *testing.T) {
	input, _, runID := stdioFixture(t)
	server := noonmcp.NewServer(input, "test")
	lines := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"stdio-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		marshalProtocolLine(t, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "get_run", "arguments": map[string]any{"run_id": runID, "wait_seconds": 60}}}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := new(bytes.Buffer)
	done := make(chan error, 1)
	go func() {
		done <- noonmcp.ServeStdio(ctx, server, strings.NewReader(strings.Join(lines, "\n")+"\n"), output)
	}()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ServeStdio shutdown error = %v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeStdio did not cancel its pending SDK request")
	}
}

func stdioFixture(t *testing.T) (config.Input, string, int64) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("HOME", root)
	userConfigDir := filepath.Join(root, "user")
	stateDir := filepath.Join(root, "state", "noon")
	t.Setenv("XDG_CONFIG_HOME", userConfigDir)
	t.Setenv("XDG_STATE_HOME", filepath.Dir(stateDir))
	input := config.Input{CWD: root, UserConfigDir: userConfigDir, StateDirDefault: stateDir}
	writeManifest(t, root, "jobs:\n  probe: {run: 'true'}\n")
	cfg, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.Enable(cfg, time.Now()); err != nil {
		t.Fatalf("enable stdio fixture: %v", err)
	}
	now := time.Now()
	queued, err := app.QueueRun(cfg, "probe", "", now.Add(time.Hour), now, time.Now, false, "test")
	if err != nil {
		t.Fatalf("queue stdio fixture run: %v", err)
	}
	return input, stateDir, queued.Run.ID
}

func serveStdio(t *testing.T, server *protocol.Server, input string, timeout time.Duration) ([]byte, time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output := new(bytes.Buffer)
	done := make(chan error, 1)
	started := time.Now()
	go func() {
		done <- noonmcp.ServeStdio(ctx, server, strings.NewReader(input), output)
	}()
	select {
	case err := <-done:
		return append([]byte(nil), output.Bytes()...), time.Since(started), err
	case <-ctx.Done():
		t.Fatalf("ServeStdio did not finish before %s", timeout)
		return nil, time.Since(started), ctx.Err()
	}
}

func marshalProtocolLine(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func protocolResponses(t *testing.T, output []byte) []map[string]any {
	t.Helper()
	var responses []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var response map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatalf("decode stdio response %q: %v", scanner.Text(), err)
		}
		responses = append(responses, response)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return responses
}

func responseForID(responses []map[string]any, id any) map[string]any {
	for _, response := range responses {
		if reflect.DeepEqual(response["id"], id) {
			return response
		}
	}
	return nil
}

func writeManifest(t *testing.T, root, manifest string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

func callTool(t *testing.T, ctx context.Context, session *protocol.ClientSession, name string, arguments map[string]any) *protocol.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &protocol.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

func decodeValue(t *testing.T, data any) any {
	t.Helper()
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatalf("decode structured content %s: %v", encoded, err)
	}
	return value
}

func decodeObject(t *testing.T, data any) map[string]any {
	t.Helper()
	object, ok := decodeValue(t, data).(map[string]any)
	if !ok {
		t.Fatalf("structured content is not an object: %#v", data)
	}
	return object
}

func textContent(result *protocol.CallToolResult) string {
	var text []string
	for _, content := range result.Content {
		if block, ok := content.(*protocol.TextContent); ok {
			text = append(text, block.Text)
		}
	}
	return strings.Join(text, "\n")
}

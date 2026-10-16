package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/service"
	"github.com/brettinternet/noon/internal/store"
)

func TestServiceCommands(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("service commands are supported on macOS and Linux")
	}
	root := sandbox(t)
	binDir := filepath.Join(root, "fake-bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	managerLog := filepath.Join(root, "manager.log")
	for _, name := range []string{"launchctl", "systemctl", "loginctl"} {
		writeFakeManager(t, binDir, name)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NOON_SERVICE_LOG", managerLog)
	t.Setenv("NOON_NO_MANAGER", "")

	stateDir := filepath.Join(root, "relative-state")
	options, err := service.CurrentConfig(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := service.CurrentUnit(options)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service", "install", "--print"}, config.BuildOpts{Version: "test"}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), stateDir) || !strings.Contains(out.String(), "serve") {
		t.Fatalf("print unit: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	if _, err := os.Stat(unit.Path); !os.IsNotExist(err) {
		t.Fatalf("--print created unit %s: %v", unit.Path, err)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("--print created state directory: %v", err)
	}
	if _, err := os.Stat(managerLog); !os.IsNotExist(err) {
		t.Fatalf("--print invoked a service manager: %v", err)
	}

	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service", "install"}, config.BuildOpts{Version: "test"}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), unit.Path) {
		t.Fatalf("install: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	if unit.Platform == service.Systemd && !strings.Contains(out.String(), "loginctl enable-linger") {
		t.Fatalf("Linger=no hint missing: %s", &out)
	}
	info, err := os.Stat(stateDir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("state directory mode: info=%v err=%v", info, err)
	}
	contents, err := os.ReadFile(unit.Path)
	if err != nil || !strings.Contains(string(contents), stateDir) || !strings.Contains(string(contents), "serve") {
		t.Fatalf("installed unit: %v contents=%s", err, contents)
	}
	firstLog := readManagerLog(t, managerLog)
	if unit.Platform == service.Launchd {
		want := []string{"launchctl\tbootstrap\tgui/" + strconv.Itoa(os.Getuid()) + "\t" + unit.Path}
		if strings.Join(firstLog, "\n") != strings.Join(want, "\n") {
			t.Fatalf("launchd install commands = %q, want %q", firstLog, want)
		}
	} else {
		want := []string{
			"systemctl\t--user\tshow-environment",
			"systemctl\t--user\tdaemon-reload",
			"systemctl\t--user\tenable\t--now\tnoon.service",
			"loginctl\tshow-user\t--property=Linger\t--value",
		}
		if strings.Join(firstLog, "\n") != strings.Join(want, "\n") {
			t.Fatalf("systemd install commands = %q, want %q", firstLog, want)
		}
	}

	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service", "install"}, config.BuildOpts{Version: "test"}, &out, &stderr)
	if code != 0 {
		t.Fatalf("repeat install: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	secondLog := readManagerLog(t, managerLog)
	newCommands := secondLog[len(firstLog):]
	if unit.Platform == service.Launchd {
		want := []string{"launchctl\tprint\tgui/" + strconv.Itoa(os.Getuid()) + "/" + service.LaunchdLabel}
		if strings.Join(newCommands, "\n") != strings.Join(want, "\n") {
			t.Fatalf("unchanged launchd install commands = %q, want %q", newCommands, want)
		}
	} else {
		want := []string{
			"systemctl\t--user\tshow-environment",
			"systemctl\t--user\tenable\t--now\tnoon.service",
			"loginctl\tshow-user\t--property=Linger\t--value",
		}
		if strings.Join(newCommands, "\n") != strings.Join(want, "\n") {
			t.Fatalf("unchanged systemd install commands = %q, want %q", newCommands, want)
		}
	}

	database, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := database.SetScheduler(store.SchedulerRecord{PID: 123, StartIdentity: "test", Version: "test", StartedAt: store.FormatTime(now), HeartbeatAt: store.FormatTime(now)}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service"}, config.BuildOpts{Version: "test"}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), "Installed:  yes") || !strings.Contains(out.String(), "Scheduler:  running") || !strings.Contains(out.String(), "Heartbeat:") {
		t.Fatalf("service status: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	// Captured PATH differs between shells; only executable and state directory drift.
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+filepath.Join(root, "other-bin"))
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service"}, config.BuildOpts{Version: "test"}, &out, &stderr)
	if code != 0 || strings.Contains(out.String(), "Warning") {
		t.Fatalf("PATH-only change reported drift: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}

	if err := os.WriteFile(unit.Path, bytes.Replace(contents, []byte(stateDir), []byte(filepath.Join(root, "drifted-state")), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service", "--json"}, config.BuildOpts{Version: "test"}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), "\"drift\":true") || !strings.Contains(out.String(), "noon service install") {
		t.Fatalf("drift status: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	// A failed update must retain the previous unit so retry replaces the old
	// running configuration rather than claiming unchanged success.
	previousUnit, err := os.ReadFile(unit.Path)
	if err != nil {
		t.Fatal(err)
	}
	failureOperation := "bootout"
	if unit.Platform == service.Systemd {
		failureOperation = "restart"
	}
	t.Setenv("NOON_FAIL_OPERATION", failureOperation)
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service", "install"}, config.BuildOpts{}, &out, &stderr)
	restored, err := os.ReadFile(unit.Path)
	if code != 1 || err != nil || !bytes.Equal(restored, previousUnit) {
		t.Fatalf("failed update did not preserve retry: exit=%d err=%v unit=%s stderr=%s", code, err, restored, &stderr)
	}
	t.Setenv("NOON_FAIL_OPERATION", "")
	beforeRepairLog := readManagerLog(t, managerLog)

	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service", "install"}, config.BuildOpts{Version: "test"}, &out, &stderr)
	if code != 0 {
		t.Fatalf("repair drift: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	repairedLog := readManagerLog(t, managerLog)
	repairCommands := repairedLog[len(beforeRepairLog):]
	if unit.Platform == service.Launchd {
		want := []string{
			"launchctl\tbootout\tgui/" + strconv.Itoa(os.Getuid()) + "\t" + unit.Path,
			"launchctl\tbootstrap\tgui/" + strconv.Itoa(os.Getuid()) + "\t" + unit.Path,
		}
		if strings.Join(repairCommands, "\n") != strings.Join(want, "\n") {
			t.Fatalf("launchd upgrade commands = %q, want %q", repairCommands, want)
		}
	} else {
		want := []string{
			"systemctl\t--user\tshow-environment",
			"systemctl\t--user\tdaemon-reload",
			"systemctl\t--user\trestart\tnoon.service",
			"loginctl\tshow-user\t--property=Linger\t--value",
		}
		if strings.Join(repairCommands, "\n") != strings.Join(want, "\n") {
			t.Fatalf("systemd upgrade commands = %q, want %q", repairCommands, want)
		}
	}

	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service", "uninstall"}, config.BuildOpts{Version: "test"}, &out, &stderr)
	if code != 0 {
		t.Fatalf("uninstall: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	uninstallLog := readManagerLog(t, managerLog)
	uninstallCommands := uninstallLog[len(repairedLog):]
	if unit.Platform == service.Launchd {
		want := []string{"launchctl\tbootout\tgui/" + strconv.Itoa(os.Getuid()) + "\t" + unit.Path}
		if strings.Join(uninstallCommands, "\n") != strings.Join(want, "\n") {
			t.Fatalf("launchd uninstall commands = %q, want %q", uninstallCommands, want)
		}
	} else {
		want := []string{
			"systemctl\t--user\tshow-environment",
			"systemctl\t--user\tdisable\t--now\tnoon.service",
			"systemctl\t--user\tdaemon-reload",
		}
		if strings.Join(uninstallCommands, "\n") != strings.Join(want, "\n") {
			t.Fatalf("systemd uninstall commands = %q, want %q", uninstallCommands, want)
		}
	}
	if _, err := os.Stat(unit.Path); !os.IsNotExist(err) {
		t.Fatalf("uninstall left unit behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "noon.db")); err != nil {
		t.Fatalf("uninstall removed state/history: %v", err)
	}
	previousLog := readManagerLog(t, managerLog)
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "--state-dir", "relative-state", "service", "uninstall"}, config.BuildOpts{Version: "test"}, &out, &stderr)
	if code != 0 || len(readManagerLog(t, managerLog)) != len(previousLog) {
		t.Fatalf("repeat uninstall: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	if unit.Platform == service.Systemd {
		t.Setenv("NOON_NO_MANAGER", "yes")
		out.Reset()
		stderr.Reset()
		code = cli.Run(context.Background(), []string{"noon", "--json", "--state-dir", "relative-state", "service", "install"}, config.BuildOpts{Version: "test"}, &out, &stderr)
		if code != 1 || !strings.Contains(out.String(), `"code":"service_unsupported"`) || !strings.Contains(out.String(), "Run noon serve under your own supervisor") {
			t.Fatalf("unsupported manager: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
		}
		if _, err := os.Stat(unit.Path); !os.IsNotExist(err) {
			t.Fatalf("unsupported install wrote service unit: %v", err)
		}
	}
}

func writeFakeManager(t *testing.T, binDir, name string) {
	t.Helper()
	script := "#!/bin/sh\nprintf '%s' \"${0##*/}\" >> \"$NOON_SERVICE_LOG\"\nfor argument do printf '\\t%s' \"$argument\" >> \"$NOON_SERVICE_LOG\"; done\nprintf '\\n' >> \"$NOON_SERVICE_LOG\"\nif [ \"$1\" = --user ]; then shift; fi\nif [ -n \"$NOON_FAIL_OPERATION\" ] && [ \"$1\" = \"$NOON_FAIL_OPERATION\" ]; then printf 'manager update failed\\n' >&2; exit 1; fi\ncase \"$1\" in\n  print) printf 'state = running\\n' ;;\n  show-environment) if [ \"$NOON_NO_MANAGER\" = yes ]; then printf 'systemd user manager unavailable\\n' >&2; exit 1; fi; printf 'PATH=%s\\n' \"$PATH\" ;;\n  is-active) printf 'active\\n' ;;\n  show-user) printf 'no\\n' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func readManagerLog(t *testing.T, path string) []string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

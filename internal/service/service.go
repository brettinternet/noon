// Package service renders and manages one per-user Noon scheduler service.
package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	LaunchdLabel = "io.github.brettinternet.noon"
	SystemdUnit  = "noon.service"
)

type Platform string

const (
	Launchd Platform = "launchd"
	Systemd Platform = "systemd"
)

type Config struct {
	StateDir      string
	Executable    string
	Path          string
	XDGConfigHome string
	Home          string
}

type Unit struct {
	Platform Platform
	Path     string
	Contents []byte
}

type InstallResult struct {
	UnitPath       string
	Changed        bool
	LingerDisabled bool
}

type Status struct {
	Installed    bool
	UnitPath     string
	ManagerState string
	Drift        bool
}

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type UnsupportedError struct{ Message string }

func (e *UnsupportedError) Error() string { return e.Message }

func IsUnsupported(err error) bool {
	var unsupported *UnsupportedError
	return errors.As(err, &unsupported)
}

func CurrentPlatform() (Platform, error) {
	switch runtime.GOOS {
	case "darwin":
		return Launchd, nil
	case "linux":
		return Systemd, nil
	default:
		return "", &UnsupportedError{Message: fmt.Sprintf("user services are not supported on %s", runtime.GOOS)}
	}
}

// CurrentConfig captures only the environment needed to run and locate the service.
func CurrentConfig(stateDir string) (Config, error) {
	executable, err := os.Executable()
	if err != nil {
		return Config{}, fmt.Errorf("resolve Noon executable: %w", err)
	}
	pathExecutable, err := ResolveExecutable(executable, os.Getenv("PATH"))
	if err != nil {
		return Config{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve user home: %w", err)
	}
	return Config{
		StateDir:      stateDir,
		Executable:    pathExecutable,
		Path:          os.Getenv("PATH"),
		XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		Home:          home,
	}, nil
}

// ResolveExecutable preserves a PATH-visible symlink when it names the running binary.
func ResolveExecutable(executable, pathValue string) (string, error) {
	executable, err := filepath.Abs(executable)
	if err != nil {
		return "", fmt.Errorf("resolve Noon executable: %w", err)
	}
	if found, err := lookPath("noon", pathValue); err == nil {
		if found, err = filepath.Abs(found); err == nil {
			foundInfo, foundErr := os.Stat(found)
			runningInfo, runningErr := os.Stat(executable)
			if foundErr == nil && runningErr == nil && os.SameFile(foundInfo, runningInfo) {
				return found, nil
			}
		}
	}
	return executable, nil
}

func lookPath(file, pathValue string) (string, error) {
	if strings.ContainsRune(file, filepath.Separator) {
		return "", exec.ErrNotFound
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, file)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}
	return "", exec.ErrNotFound
}

func BuildUnit(platform Platform, config Config) (Unit, error) {
	if !filepath.IsAbs(config.StateDir) {
		return Unit{}, fmt.Errorf("state directory must be absolute: %q", config.StateDir)
	}
	if !filepath.IsAbs(config.Executable) {
		return Unit{}, fmt.Errorf("executable path must be absolute: %q", config.Executable)
	}
	if config.XDGConfigHome != "" && !filepath.IsAbs(config.XDGConfigHome) {
		return Unit{}, fmt.Errorf("XDG_CONFIG_HOME must be absolute: %q", config.XDGConfigHome)
	}
	switch platform {
	case Launchd:
		return buildLaunchd(config)
	case Systemd:
		return buildSystemd(config)
	default:
		return Unit{}, &UnsupportedError{Message: fmt.Sprintf("user services are not supported on %q", platform)}
	}
}

func CurrentUnit(config Config) (Unit, error) {
	platform, err := CurrentPlatform()
	if err != nil {
		return Unit{}, err
	}
	if config.Home == "" {
		return Unit{}, errors.New("user home directory is required")
	}
	unit, err := BuildUnit(platform, config)
	if err != nil {
		return Unit{}, err
	}
	if platform == Launchd {
		unit.Path = filepath.Join(config.Home, "Library", "LaunchAgents", LaunchdLabel+".plist")
	} else {
		configHome := config.XDGConfigHome
		if configHome == "" {
			configHome = filepath.Join(config.Home, ".config")
		}
		unit.Path = filepath.Join(configHome, "systemd", "user", SystemdUnit)
	}
	return unit, nil
}

func buildLaunchd(config Config) (Unit, error) {
	var body bytes.Buffer
	fmt.Fprintln(&body, `<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintln(&body, `<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`)
	fmt.Fprintln(&body, `<plist version="1.0">`)
	fmt.Fprintln(&body, `<dict>`)
	if err := plistString(&body, "Label", LaunchdLabel); err != nil {
		return Unit{}, err
	}
	fmt.Fprintln(&body, "  <key>ProgramArguments</key>")
	fmt.Fprintln(&body, "  <array>")
	for _, arg := range []string{config.Executable, "--state-dir", config.StateDir, "serve"} {
		encoded, err := xmlText(arg)
		if err != nil {
			return Unit{}, err
		}
		fmt.Fprintf(&body, "    <string>%s</string>\n", encoded)
	}
	fmt.Fprintln(&body, "  </array>")
	fmt.Fprintln(&body, "  <key>EnvironmentVariables</key>")
	fmt.Fprintln(&body, "  <dict>")
	if err := plistString(&body, "PATH", config.Path); err != nil {
		return Unit{}, err
	}
	if config.XDGConfigHome != "" {
		if err := plistString(&body, "XDG_CONFIG_HOME", config.XDGConfigHome); err != nil {
			return Unit{}, err
		}
	}
	fmt.Fprintln(&body, "  </dict>")
	fmt.Fprintln(&body, "  <key>RunAtLoad</key><true/>")
	fmt.Fprintln(&body, "  <key>KeepAlive</key><true/>")
	fmt.Fprintln(&body, "  <key>ThrottleInterval</key><integer>10</integer>")
	logPath := filepath.Join(config.StateDir, "scheduler.log")
	if err := plistString(&body, "StandardOutPath", logPath); err != nil {
		return Unit{}, err
	}
	if err := plistString(&body, "StandardErrorPath", logPath); err != nil {
		return Unit{}, err
	}
	fmt.Fprintln(&body, `</dict>`)
	fmt.Fprintln(&body, `</plist>`)
	return Unit{Platform: Launchd, Contents: body.Bytes()}, nil
}

func plistString(out *bytes.Buffer, key, value string) error {
	encodedKey, err := xmlText(key)
	if err != nil {
		return err
	}
	encodedValue, err := xmlText(value)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "  <key>%s</key><string>%s</string>\n", encodedKey, encodedValue)
	return nil
}

func xmlText(value string) (string, error) {
	var encoded bytes.Buffer
	if err := xml.EscapeText(&encoded, []byte(value)); err != nil {
		return "", err
	}
	return encoded.String(), nil
}

func buildSystemd(config Config) (Unit, error) {
	var body strings.Builder
	body.WriteString("[Unit]\nDescription=Noon per-user scheduler\n\n[Service]\nType=simple\n")
	// ExecStart expands environment variables even inside quoted arguments.
	body.WriteString("ExecStart=" + systemdQuote(strings.ReplaceAll(config.Executable, "$", "$$")) + " --state-dir " + systemdQuote(strings.ReplaceAll(config.StateDir, "$", "$$")) + " serve\n")
	body.WriteString("Environment=" + systemdQuote("PATH="+config.Path) + "\n")
	if config.XDGConfigHome != "" {
		body.WriteString("Environment=" + systemdQuote("XDG_CONFIG_HOME="+config.XDGConfigHome) + "\n")
	}
	body.WriteString("Restart=always\nRestartSec=5\nKillMode=mixed\nTimeoutStopSec=30\n\n[Install]\nWantedBy=default.target\n")
	return Unit{Platform: Systemd, Contents: []byte(body.String())}, nil
}

// systemdQuote quotes one word for systemd's unit-file command and environment parser.
func systemdQuote(value string) string {
	var result strings.Builder
	result.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			result.WriteString(`\\`)
		case '"':
			result.WriteString(`\"`)
		case '\n':
			result.WriteString(`\n`)
		case '\r':
			result.WriteString(`\r`)
		case '\t':
			result.WriteString(`\t`)
		case '%':
			result.WriteString("%%")
		default:
			result.WriteRune(r)
		}
	}
	result.WriteByte('"')
	return result.String()
}

func Install(ctx context.Context, config Config, runner Runner) (result InstallResult, err error) {
	unit, err := CurrentUnit(config)
	if err != nil {
		return InstallResult{}, err
	}
	if err := os.MkdirAll(config.StateDir, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("create state directory: %w", err)
	}
	if err := os.Chmod(config.StateDir, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("secure state directory: %w", err)
	}
	if unit.Platform == Systemd {
		if _, err := runner.Run(ctx, "systemctl", "--user", "show-environment"); err != nil {
			return InstallResult{}, unsupportedManager("systemd user manager is unavailable", err)
		}
	}
	installed, previous, err := readUnit(unit.Path)
	if err != nil {
		return InstallResult{}, err
	}
	changed := !installed || !bytes.Equal(previous, unit.Contents)
	if err := writeUnit(unit.Path, unit.Contents, changed); err != nil {
		return InstallResult{}, err
	}
	// Keep disk content as the retry marker: a failed manager update must not
	// look like an unchanged successful install on the next invocation.
	defer func() {
		if err == nil || !changed {
			return
		}
		var rollbackErr error
		if installed {
			rollbackErr = writeUnit(unit.Path, previous, true)
		} else {
			rollbackErr = os.Remove(unit.Path)
		}
		if rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("restore service unit after failed install: %w", rollbackErr))
		}
	}()
	result = InstallResult{UnitPath: unit.Path, Changed: changed}
	if unit.Platform == Launchd {
		if changed {
			if err := os.MkdirAll(filepath.Dir(unit.Path), 0o700); err != nil {
				return InstallResult{}, fmt.Errorf("create launch agent directory: %w", err)
			}
			if installed {
				if _, err := runner.Run(ctx, "launchctl", "bootout", launchDomain(), unit.Path); err != nil && !notLoaded(err) {
					return InstallResult{}, err
				}
			}
			if _, err := runner.Run(ctx, "launchctl", "bootstrap", launchDomain(), unit.Path); err != nil {
				return InstallResult{}, err
			}
		} else if _, err := runner.Run(ctx, "launchctl", "print", launchDomain()+"/"+LaunchdLabel); err != nil {
			if notLoaded(err) {
				if _, loadErr := runner.Run(ctx, "launchctl", "bootstrap", launchDomain(), unit.Path); loadErr != nil {
					return InstallResult{}, loadErr
				}
			} else {
				return InstallResult{}, err
			}
		}
	} else {
		if changed {
			if err := os.MkdirAll(filepath.Dir(unit.Path), 0o700); err != nil {
				return InstallResult{}, fmt.Errorf("create systemd user unit directory: %w", err)
			}
			if _, err := runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
				return InstallResult{}, err
			}
			if installed {
				_, err = runner.Run(ctx, "systemctl", "--user", "restart", SystemdUnit)
			} else {
				_, err = runner.Run(ctx, "systemctl", "--user", "enable", "--now", SystemdUnit)
			}
			if err != nil {
				return InstallResult{}, err
			}
		} else if _, err := runner.Run(ctx, "systemctl", "--user", "enable", "--now", SystemdUnit); err != nil {
			return InstallResult{}, err
		}
		if output, err := runner.Run(ctx, "loginctl", "show-user", "--property=Linger", "--value"); err == nil {
			result.LingerDisabled = strings.EqualFold(strings.TrimSpace(string(output)), "no")
		}
	}
	return result, nil
}

// Restart restarts an installed unit without changing its contents or environment.
// Only Platform and Path are used; Contents is never installed.
func Restart(ctx context.Context, unit Unit, runner Runner) error {
	if unit.Platform != Launchd && unit.Platform != Systemd {
		return &UnsupportedError{Message: fmt.Sprintf("user services are not supported on %q", unit.Platform)}
	}
	installed, _, err := readUnit(unit.Path)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("service is not installed: %s; run noon service install first", unit.Path)
	}
	if unit.Platform == Launchd {
		if _, err := runner.Run(ctx, "launchctl", "bootout", launchDomain(), unit.Path); err != nil && !notLoaded(err) {
			return err
		}
		_, err = runner.Run(ctx, "launchctl", "bootstrap", launchDomain(), unit.Path)
		return err
	}
	if _, err := runner.Run(ctx, "systemctl", "--user", "show-environment"); err != nil {
		return unsupportedManager("systemd user manager is unavailable", err)
	}
	_, err = runner.Run(ctx, "systemctl", "--user", "restart", SystemdUnit)
	return err
}

func IsInstalled(config Config) (string, bool, error) {
	unit, err := CurrentUnit(config)
	if err != nil {
		return "", false, err
	}
	installed, _, err := readUnit(unit.Path)
	return unit.Path, installed, err
}

func Uninstall(ctx context.Context, config Config, runner Runner) (string, bool, error) {
	unit, err := CurrentUnit(config)
	if err != nil {
		return "", false, err
	}
	installed, _, err := readUnit(unit.Path)
	if err != nil {
		return unit.Path, false, err
	}
	if !installed {
		return unit.Path, false, nil
	}
	if unit.Platform == Launchd {
		if _, err := runner.Run(ctx, "launchctl", "bootout", launchDomain(), unit.Path); err != nil && !notLoaded(err) {
			return unit.Path, true, err
		}
	} else {
		if _, err := runner.Run(ctx, "systemctl", "--user", "show-environment"); err != nil {
			return unit.Path, true, unsupportedManager("systemd user manager is unavailable", err)
		}
		if _, err := runner.Run(ctx, "systemctl", "--user", "disable", "--now", SystemdUnit); err != nil {
			return unit.Path, true, err
		}
	}
	if err := os.Remove(unit.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return unit.Path, true, fmt.Errorf("remove service unit: %w", err)
	}
	if unit.Platform == Systemd {
		if _, err := runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return unit.Path, true, err
		}
	}
	return unit.Path, true, nil
}

func Inspect(ctx context.Context, config Config, runner Runner) (Status, error) {
	unit, err := CurrentUnit(config)
	if err != nil {
		return Status{}, err
	}
	installed, contents, err := readUnit(unit.Path)
	if err != nil {
		return Status{}, err
	}
	status := Status{Installed: installed, UnitPath: unit.Path}
	if installed {
		status.Drift = !bytes.Contains(contents, identity(unit))
	}
	if unit.Platform == Systemd {
		if _, err := runner.Run(ctx, "systemctl", "--user", "show-environment"); err != nil {
			return Status{}, unsupportedManager("systemd user manager is unavailable", err)
		}
		output, err := runner.Run(ctx, "systemctl", "--user", "is-active", SystemdUnit)
		status.ManagerState = strings.TrimSpace(string(output))
		if status.ManagerState == "" {
			if err != nil {
				status.ManagerState = "inactive"
			} else {
				status.ManagerState = "unknown"
			}
		}
	} else {
		output, err := runner.Run(ctx, "launchctl", "print", launchDomain()+"/"+LaunchdLabel)
		if err != nil {
			if !notLoaded(err) {
				return Status{}, err
			}
			status.ManagerState = "not loaded"
		} else {
			status.ManagerState = launchdState(string(output))
		}
	}
	return status, nil
}

// identity returns the rendered fragment naming the executable and state
// directory. Drift ignores captured PATH, which varies between shells.
func identity(unit Unit) []byte {
	start, end := []byte("\nExecStart="), []byte("\n")
	if unit.Platform == Launchd {
		start, end = []byte("<key>ProgramArguments</key>"), []byte("</array>")
	}
	i := bytes.Index(unit.Contents, start)
	j := bytes.Index(unit.Contents[i+len(start):], end)
	return unit.Contents[i : i+len(start)+j+len(end)]
}

func readUnit(path string) (bool, []byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("inspect service unit: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, nil, fmt.Errorf("service unit is not a regular file: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return false, nil, fmt.Errorf("read service unit: %w", err)
	}
	return true, contents, nil
}

func writeUnit(path string, contents []byte, changed bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create service unit directory: %w", err)
	}
	if !changed {
		return nil
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".noon-service-*")
	if err != nil {
		return fmt.Errorf("create temporary service unit: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary service unit: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write service unit: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync service unit: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close service unit: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("install service unit: %w", err)
	}
	return nil
}

func launchDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func launchdState(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if key, value, found := strings.Cut(strings.TrimSpace(line), "="); found && strings.TrimSpace(key) == "state" {
			return strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(output) != "" {
		return "loaded"
	}
	return "unknown"
}

func notLoaded(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "could not find service") || strings.Contains(text, "could not find specified service") || strings.Contains(text, "service not found") || strings.Contains(text, "no such process")
}

func unsupportedManager(message string, err error) error {
	return &UnsupportedError{Message: message + ": " + err.Error()}
}

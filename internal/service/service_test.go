package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/brettinternet/noon/internal/config"
)

func TestServiceUnits(t *testing.T) {
	installDir := filepath.Join(t.TempDir(), "install here")
	relativeState := "relative-state"
	stateDir, err := config.StateDirectory(config.Input{CWD: installDir, StateDirDefault: filepath.Join(installDir, "unused"), StateDir: &relativeState})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{
		StateDir: stateDir, Executable: "/Applications/Noon Tool/noon",
		Path: "/opt/homebrew/bin:/usr/bin", XDGConfigHome: "/Users/test person/.config",
		Home: "/Users/test person",
	}
	for _, platform := range []Platform{Launchd, Systemd} {
		t.Run(string(platform), func(t *testing.T) {
			unit, err := BuildUnit(platform, config)
			if err != nil {
				t.Fatal(err)
			}
			text := string(unit.Contents)
			for _, expected := range []string{config.Executable, "--state-dir", config.StateDir, "serve", "PATH", config.Path, "XDG_CONFIG_HOME", config.XDGConfigHome} {
				if !strings.Contains(text, expected) {
					t.Errorf("rendered %s unit is missing %q:\n%s", platform, expected, text)
				}
			}
			for _, forbidden := range []string{"NOON_STATE_DIR", "XDG_STATE_HOME", "NOON_TOKEN", "TOKEN=secret"} {
				if strings.Contains(text, forbidden) {
					t.Errorf("rendered %s unit contains uncaptured environment %q", platform, forbidden)
				}
			}
			if platform == Launchd {
				for _, expected := range []string{"<key>RunAtLoad</key><true/>", "<key>KeepAlive</key><true/>", "<key>ThrottleInterval</key><integer>10</integer>", "scheduler.log"} {
					if !strings.Contains(text, expected) {
						t.Errorf("rendered plist is missing %q:\n%s", expected, text)
					}
				}
				if runtime.GOOS == "darwin" {
					path := filepath.Join(t.TempDir(), "noon.plist")
					if err := os.WriteFile(path, unit.Contents, 0o600); err != nil {
						t.Fatal(err)
					}
					output, err := exec.Command("plutil", "-lint", path).CombinedOutput()
					if err != nil {
						t.Fatalf("plutil -lint: %v: %s", err, output)
					}
				}
			} else {
				for _, expected := range []string{
					`ExecStart="/Applications/Noon Tool/noon" --state-dir "` + stateDir + `" serve`,
					"Restart=always", "RestartSec=5", "KillMode=mixed", "TimeoutStopSec=30", "WantedBy=default.target",
				} {
					if !strings.Contains(text, expected) {
						t.Errorf("rendered systemd unit is missing %q:\n%s", expected, text)
					}
				}
			}
		})
		withoutConfigHome := config
		withoutConfigHome.XDGConfigHome = ""
		unit, err := BuildUnit(platform, withoutConfigHome)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(unit.Contents), "XDG_CONFIG_HOME") {
			t.Errorf("%s unit includes unset XDG_CONFIG_HOME:\n%s", platform, unit.Contents)
		}
	}
}

// Literal dollars in paths must not select a different state store at startup.
func TestSystemdLiteralDollars(t *testing.T) {
	unit, err := BuildUnit(Systemd, Config{
		StateDir: "/home/user/${HOME}/$state", Executable: "/opt/$tools/noon",
		Path: "/opt/$tools:${HOME}/bin", XDGConfigHome: "/home/$user/.config",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`ExecStart="/opt/$$tools/noon" --state-dir "/home/user/$${HOME}/$$state" serve`,
		`Environment="PATH=/opt/$tools:${HOME}/bin"`,
		`Environment="XDG_CONFIG_HOME=/home/$user/.config"`,
	} {
		if !strings.Contains(string(unit.Contents), want) {
			t.Fatalf("missing literal value %q in unit:\n%s", want, unit.Contents)
		}
	}
}

func TestResolveExecutablePreservesPATHSymlink(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "real-noon")
	if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(linkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "noon")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveExecutable(executable, linkDir)
	if err != nil || got != link {
		t.Fatalf("PATH symlink = %q, %v; want %q", got, err, link)
	}
	other := filepath.Join(dir, "other-noon")
	if err := os.WriteFile(other, []byte("other"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveExecutable(executable, linkDir)
	if err != nil || got != executable {
		t.Fatalf("different PATH executable = %q, %v; want running executable %q", got, err, executable)
	}
}

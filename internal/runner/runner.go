// Package runner executes one command with a bounded process-group lifecycle.
package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Spec struct {
	Argv               []string
	Dir                string
	Env                []string
	Stdin              io.Reader // nil is /dev/null
	Stdout, Stderr     io.Writer
	Log                io.Writer
	OnStart            func(pid, pgid int, leaderStart string) error
	Timeout, KillGrace time.Duration
	// ForceKill requests immediate group termination after a second signal.
	ForceKill <-chan struct{}
}

type Result struct {
	ExitCode              int
	Signal                string
	TimedOut, Cancelled   bool
	StartError            error
	StartedAt, FinishedAt time.Time
	PID, PGID             int
}

// Run waits for the leader, terminates remaining group members, and drains output.
// Writers must return from Write; stdout and stderr writes are serialized.
func Run(ctx context.Context, spec Spec) (result Result) {
	result.StartedAt = time.Now()
	defer func() { result.FinishedAt = time.Now() }()
	fail := func(err error, code int) Result {
		result.StartError, result.ExitCode = err, code
		return result
	}
	if ctx.Err() != nil {
		result.Cancelled, result.ExitCode = true, 130
		return result
	}
	dir := spec.Dir
	if dir == "" {
		dir = "."
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return fail(err, 125)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fail(err, 125)
	}
	if !info.IsDir() {
		return fail(syscall.ENOTDIR, 125)
	}
	if len(spec.Argv) == 0 {
		return fail(errors.New("empty argv"), 125)
	}
	path, err := ResolveExecutable(spec.Argv[0], spec.Env, dir)
	if err != nil {
		return fail(err, startCode(err))
	}
	grace := spec.KillGrace
	if grace <= 0 {
		grace = 10 * time.Second
	}
	cmd := exec.Command(path, spec.Argv[1:]...)
	cmd.Args = append([]string(nil), spec.Argv...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, append([]string{}, spec.Env...), spec.Stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = grace
	// Own the pipes: Cmd.Wait must observe leader exit independently of EOF.
	outR, outW, err := os.Pipe()
	if err != nil {
		return fail(err, 125)
	}
	defer outR.Close()
	defer outW.Close()
	errR, errW, err := os.Pipe()
	if err != nil {
		return fail(err, 125)
	}
	defer errR.Close()
	defer errW.Close()
	cmd.Stdout, cmd.Stderr = outW, errW
	if err := cmd.Start(); err != nil {
		return fail(err, startCode(err))
	}
	result.PID, result.PGID = cmd.Process.Pid, cmd.Process.Pid
	_ = outW.Close()
	_ = errW.Close()
	identity, _ := ProcessStartIdentity(result.PID)
	if spec.OnStart != nil {
		if err := spec.OnStart(result.PID, result.PGID, identity); err != nil {
			_ = syscall.Kill(-result.PGID, syscall.SIGKILL)
			_ = cmd.Wait()
			return fail(err, 125)
		}
	}
	var writes sync.Mutex
	var copies sync.WaitGroup
	for _, stream := range []struct {
		r *os.File
		w io.Writer
	}{{outR, outputWriter(spec.Stdout, spec.Log)}, {errR, outputWriter(spec.Stderr, spec.Log)}} {
		copies.Add(1)
		go func(r *os.File, w io.Writer) {
			defer copies.Done()
			if w == nil {
				w = io.Discard
			}
			_, _ = io.Copy(lockedWriter{&writes, w}, r)
			// A failed destination must not block the child on a full pipe.
			_, _ = io.Copy(io.Discard, r)
		}(stream.r, stream.w)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var timeout <-chan time.Time
	if spec.Timeout > 0 {
		timer := time.NewTimer(spec.Timeout)
		defer timer.Stop()
		timeout = timer.C
	}
	var waitErr error
	leaderDone := false
	select {
	case waitErr = <-waited:
		leaderDone = true
	case <-ctx.Done():
		result.Cancelled = true
	case <-timeout:
		result.TimedOut = true
	}
	// Always clean up descendants, including after a successful leader exit.
	_ = syscall.Kill(-result.PGID, syscall.SIGTERM)
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	force := spec.ForceKill
cleanup:
	for {
		if errors.Is(syscall.Kill(-result.PGID, 0), syscall.ESRCH) {
			break
		}
		select {
		case <-deadline.C:
			_ = syscall.Kill(-result.PGID, syscall.SIGKILL)
			break cleanup
		case <-force:
			_ = syscall.Kill(-result.PGID, syscall.SIGKILL)
			break cleanup
		case <-poll.C:
		}
	}
	if !leaderDone {
		waitErr = <-waited
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			result.Signal = signalName(status.Signal())
			result.ExitCode = 128 + int(status.Signal())
		}
	} else if waitErr != nil {
		result.ExitCode = 125
	}
	if result.TimedOut {
		result.ExitCode = 124
	}
	if result.Cancelled {
		result.ExitCode = 130
	}
	// An escaped descendant may still hold a pipe. Preserve buffered output but
	// bound draining even when the inherited descriptor never reaches EOF.
	drained := make(chan struct{})
	go func() { copies.Wait(); close(drained) }()
	drainTimer := time.NewTimer(100 * time.Millisecond)
	defer drainTimer.Stop()
	select {
	case <-drained:
	case <-drainTimer.C:
		_ = outR.Close()
		_ = errR.Close()
		<-drained
	}
	return result
}

func outputWriter(stream, log io.Writer) io.Writer {
	if log == nil {
		return stream
	}
	return &teeWriter{stream: stream, log: log}
}

type teeWriter struct{ stream, log io.Writer }

func (w *teeWriter) Write(p []byte) (int, error) {
	if w.stream != nil {
		n, err := w.stream.Write(p)
		if err != nil || n != len(p) {
			w.stream = nil
		}
	}
	return w.log.Write(p)
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func startCode(err error) int {
	switch {
	case errors.Is(err, os.ErrNotExist), errors.Is(err, exec.ErrNotFound):
		return 127
	case errors.Is(err, os.ErrPermission), errors.Is(err, syscall.ENOEXEC), errors.Is(err, syscall.EISDIR):
		return 126
	default:
		return 125
	}
}

// ResolveExecutable uses the job's PATH and cwd, never the caller's working directory.
func ResolveExecutable(name string, env []string, dir string) (string, error) {
	check := func(path string) (string, error) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if info.IsDir() || info.Mode().Perm()&0111 == 0 {
			return "", &os.PathError{Op: "exec", Path: path, Err: syscall.EACCES}
		}
		return path, nil
	}
	if strings.Contains(name, "/") {
		return check(name)
	}
	path, found := "", false
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			path, found = value, true
		}
	}
	if !found || name == "" {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	var denied error
	for _, entry := range strings.Split(path, ":") {
		resolved, err := check(filepath.Join(entry, name))
		if err == nil {
			return resolved, nil
		}
		if errors.Is(err, os.ErrPermission) {
			denied = err
		}
	}
	if denied != nil {
		return "", denied
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func signalName(sig syscall.Signal) string {
	// Stable names rather than platform-specific prose from Signal.String.
	for _, pair := range []struct {
		s    syscall.Signal
		name string
	}{
		{syscall.SIGHUP, "SIGHUP"}, {syscall.SIGINT, "SIGINT"}, {syscall.SIGQUIT, "SIGQUIT"},
		{syscall.SIGILL, "SIGILL"}, {syscall.SIGTRAP, "SIGTRAP"}, {syscall.SIGABRT, "SIGABRT"},
		{syscall.SIGBUS, "SIGBUS"}, {syscall.SIGFPE, "SIGFPE"}, {syscall.SIGKILL, "SIGKILL"},
		{syscall.SIGUSR1, "SIGUSR1"}, {syscall.SIGSEGV, "SIGSEGV"}, {syscall.SIGUSR2, "SIGUSR2"},
		{syscall.SIGPIPE, "SIGPIPE"}, {syscall.SIGALRM, "SIGALRM"}, {syscall.SIGTERM, "SIGTERM"},
	} {
		if sig == pair.s {
			return pair.name
		}
	}
	return sig.String()
}

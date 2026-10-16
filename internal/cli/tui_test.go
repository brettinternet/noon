package cli_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
)

func TestTUINonInteractive(t *testing.T) {
	oldStdin, oldStdout := os.Stdin, os.Stdout
	stdin, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		stdin.Close()
		stdinWriter.Close()
		t.Fatal(err)
	}
	stdinWriter.Close()
	os.Stdin, os.Stdout = stdin, stdoutWriter
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldStdin, oldStdout
		stdin.Close()
		stdoutReader.Close()
		stdoutWriter.Close()
	})

	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"noon", "tui"}, want: "tui needs a terminal; use noon status"},
		{args: []string{"noon", "tui", "--json"}, want: "tui does not support --json"},
	} {
		t.Run(strings.Join(tc.args[1:], "-"), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var stdout, stderr bytes.Buffer
			code := cli.Run(ctx, tc.args, config.BuildOpts{}, &stdout, &stderr)
			if code != 2 {
				t.Fatalf("exit=%d stdout=%q stderr=%q; want usage error containing %q", code, stdout.String(), stderr.String(), tc.want)
			}
			if strings.Contains(tc.args[len(tc.args)-1], "--json") {
				if !strings.Contains(stdout.String(), tc.want) || stderr.Len() != 0 {
					t.Fatalf("JSON usage error: stdout=%q stderr=%q; want %q", stdout.String(), stderr.String(), tc.want)
				}
			} else if !strings.Contains(stderr.String(), tc.want) || stdout.Len() != 0 {
				t.Fatalf("human usage error: stdout=%q stderr=%q; want %q", stdout.String(), stderr.String(), tc.want)
			}
		})
	}
}

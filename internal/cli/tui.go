package cli

import (
	"context"
	"errors"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/brettinternet/noon/internal/tui"
	"github.com/mattn/go-isatty"
	urfave "github.com/urfave/cli/v3"
)

func tuiAction(ctx context.Context, cmd *urfave.Command) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	if cmd.Bool("json") {
		return usageError("tui does not support --json", "Run noon tui in a terminal")
	}
	if !terminal(os.Stdin) || !terminalWriter(cmd.Writer) {
		return usageError("tui needs a terminal; use noon status", "")
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	model, err := tui.NewModel(values)
	if err != nil {
		return err
	}
	program := tea.NewProgram(model,
		tea.WithContext(ctx),
		tea.WithInput(os.Stdin),
		tea.WithOutput(cmd.Writer),
	)
	_, err = program.Run()
	if ctx.Err() != nil || errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

func terminalWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	return ok && terminal(file)
}

func terminal(file *os.File) bool {
	return isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd())
}

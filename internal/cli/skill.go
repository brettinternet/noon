package cli

import (
	"context"
	"io"

	"github.com/brettinternet/noon/internal/skill"
	urfave "github.com/urfave/cli/v3"
)

func showSkill(_ context.Context, cmd *urfave.Command) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"content": skill.Content()})
	}
	_, err := io.WriteString(cmd.Writer, skill.Content())
	return err
}

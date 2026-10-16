package cli

import (
	"context"
	"fmt"
	"os"

	noonmcp "github.com/brettinternet/noon/internal/mcp"
	"github.com/mattn/go-isatty"
	urfave "github.com/urfave/cli/v3"
)

func mcpCommand(version string) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if err := noArgs(cmd); err != nil {
			return err
		}
		if cmd.Bool("json") {
			return usageError("mcp does not support --json", "MCP protocol messages use stdout")
		}
		if isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd()) {
			fmt.Fprintln(cmd.Writer, "noon mcp is a stdio MCP server. Start it from an MCP client:")
			fmt.Fprintln(cmd.Writer, "  claude mcp add noon -- noon mcp")
			fmt.Fprintln(cmd.Writer, "  codex mcp add noon -- noon mcp")
			fmt.Fprintln(cmd.Writer, "")
			fmt.Fprintln(cmd.Writer, "Other clients:")
			fmt.Fprintln(cmd.Writer, `{"mcpServers":{"noon":{"command":"noon","args":["mcp"]}}}`)
			return nil
		}
		values, err := input(cmd)
		if err != nil {
			return err
		}
		server := noonmcp.NewServer(values, version)
		return noonmcp.ServeStdio(ctx, server, os.Stdin, cmd.Writer)
	}
}

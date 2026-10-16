package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/brettinternet/bump/internal/cli"
	"github.com/brettinternet/bump/internal/config"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildTime    = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return cli.Run(ctx, os.Args, config.BuildOpts{
		Version: buildVersion, Commit: buildCommit, BuildTime: buildTime,
	}, os.Stdout, os.Stderr)
}

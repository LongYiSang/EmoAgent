package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/longyisang/emoagent/internal/app"
)

type appRunner interface {
	Init(ctx context.Context, configPath string) error
	Run(ctx context.Context) error
	Shutdown() error
}

func main() {
	configPath := flag.String("config", "./config.yaml", "path to config file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(runApp(ctx, app.New(), *configPath, slog.Default()))
}

func runApp(ctx context.Context, a appRunner, configPath string, logger *slog.Logger) int {
	if err := a.Init(ctx, configPath); err != nil {
		logger.Error("failed to initialize", "error", err)
		return 1
	}

	exitCode := 0
	if err := a.Run(ctx); err != nil {
		logger.Error("runtime error", "error", err)
		exitCode = 1
	}
	if err := a.Shutdown(); err != nil {
		logger.Error("shutdown error", "error", err)
		exitCode = 1
	}

	return exitCode
}

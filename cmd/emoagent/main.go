package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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

type appSelfTester interface {
	SelfTest(ctx context.Context) (app.SelfTestReport, error)
}

func main() {
	configPath := flag.String("config", "./config.yaml", "path to config file")
	selfTest := flag.Bool("self-test", false, "initialize, run local diagnostics, then exit")
	selfTestJSON := flag.Bool("self-test-json", false, "write self-test diagnostics as JSON to stdout")
	selfTestStrict := flag.Bool("self-test-strict", false, "return non-zero unless self-test status is ok")
	cleanStaleFacts := flag.Bool("clean-stale-summary-facts", false, "report running_summary facts whose wording misdates itself, then exit")
	cleanStaleFactsApply := flag.Bool("clean-stale-summary-facts-apply", false, "remove the facts reported by --clean-stale-summary-facts (stop the server first)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *cleanStaleFacts || *cleanStaleFactsApply {
		os.Exit(runCleanStaleSummaryFacts(ctx, *configPath, slog.Default(), os.Stdout, *cleanStaleFactsApply))
	}

	instance := app.New()
	if *selfTest || *selfTestJSON || *selfTestStrict {
		os.Exit(runAppSelfTest(ctx, instance, *configPath, slog.Default(), os.Stdout, *selfTestJSON, *selfTestStrict))
	}
	os.Exit(runApp(ctx, instance, *configPath, slog.Default()))
}

func runApp(ctx context.Context, a appRunner, configPath string, logger *slog.Logger) int {
	if err := a.Init(ctx, configPath); err != nil {
		logger.Error("failed to initialize", "error", err)
		return 1
	}
	runStartupSelfTest(ctx, a, logger)

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

func runStartupSelfTest(ctx context.Context, a appRunner, logger *slog.Logger) {
	tester, ok := a.(appSelfTester)
	if !ok {
		logger.Warn("startup self-test unavailable")
		return
	}
	report, err := tester.SelfTest(ctx)
	if err != nil {
		logger.Warn("startup self-test failed", "error", err)
		return
	}
	if report.Status != "ok" {
		logger.Warn("startup self-test reported non-ok status", "status", report.Status)
		return
	}
	logger.Info("startup self-test passed", "status", report.Status)
}

func runAppSelfTest(ctx context.Context, a appRunner, configPath string, logger *slog.Logger, out io.Writer, jsonOutput bool, strict bool) int {
	if err := a.Init(ctx, configPath); err != nil {
		logger.Error("failed to initialize", "error", err)
		return 1
	}

	exitCode := 0
	var report app.SelfTestReport
	var err error
	tester, ok := a.(appSelfTester)
	if !ok {
		err = fmt.Errorf("app does not support self-test")
	} else {
		report, err = tester.SelfTest(ctx)
	}
	if err != nil {
		logger.Error("self-test failed", "error", err)
		exitCode = 1
	} else if strict && report.Status != "ok" {
		logger.Error("self-test reported non-ok status", "status", report.Status)
		exitCode = 1
	}

	if err == nil {
		if jsonOutput {
			encoder := json.NewEncoder(out)
			encoder.SetIndent("", "  ")
			if encodeErr := encoder.Encode(report); encodeErr != nil {
				logger.Error("write self-test report", "error", encodeErr)
				exitCode = 1
			}
		} else {
			_, _ = fmt.Fprintf(out, "self-test status: %s\n", report.Status)
			for _, check := range report.PluginDiagnostics.Checks {
				_, _ = fmt.Fprintf(out, "%s: %s - %s\n", check.ID, check.Status, check.Message)
			}
		}
	}

	if shutdownErr := a.Shutdown(); shutdownErr != nil {
		logger.Error("shutdown error", "error", shutdownErr)
		exitCode = 1
	}
	return exitCode
}

package app

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/joho/godotenv"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/logger"
	"github.com/longyisang/emoagent/internal/memoryruntime"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
)

type Bootstrapper struct {
	ConfigPath  string
	ProjectRoot string
}

func (b Bootstrapper) Build(ctx context.Context) (kernel *Kernel, cancel context.CancelFunc, err error) {
	_ = godotenv.Load()

	cfg, err := config.Load(b.ConfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	log := logger.InitWithTimezone(cfg.Log.Level, cfg.Log.Format, cfg.Time.Timezone)
	log.Info("config loaded", "path", b.ConfigPath)

	db, err := storage.OpenWithOptions(cfg.DB.Path, log, storage.StorageOptions{Timezone: cfg.Time.Timezone})
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	projectRoot := b.ProjectRoot
	if projectRoot == "" {
		projectRoot, err = os.Getwd()
		if err != nil {
			return nil, nil, fmt.Errorf("get working directory: %w", err)
		}
	}

	kernel = NewKernel(&Infra{
		Config:      cfg,
		DB:          db,
		Logger:      log,
		ProjectRoot: projectRoot,
		Environment: runtimeenv.BuildEnvironmentFacts(runtime.GOOS, projectRoot, cfg.Bash),
	})
	createdKernel := kernel
	success := false
	defer func() {
		if !success && createdKernel != nil {
			_ = createdKernel.Close(context.Background())
			kernel = nil
			cancel = nil
		}
	}()

	if err := kernel.Services.Config.ApplyRuntimeOverrides(); err != nil {
		log.Warn("runtime config overrides failed", "error", err)
	}
	cfg = kernel.Infra.Config
	if err := kernel.Services.Personas.LoadAndSync(); err != nil {
		return nil, nil, err
	}
	if err := kernel.Services.AgentRuntime.BootstrapAgentConfigs(); err != nil {
		return nil, nil, fmt.Errorf("bootstrap agent configs: %w", err)
	}
	if err := kernel.Services.AgentRuntime.LoadActive(); err != nil {
		return nil, nil, fmt.Errorf("load active agent config: %w", err)
	}

	watchCtx, watchCancel := context.WithCancel(ctx)
	cancel = watchCancel
	kernel.Background.Cancel = cancel
	kernel.Services.Personas.Watch(watchCtx)

	if err := kernel.Services.Tools.EnsureRegistry(); err != nil {
		return nil, nil, err
	}
	if cfg.Memory.Enabled {
		if err := kernel.Services.Memory.Open(ctx); err != nil {
			return nil, nil, err
		}
	} else {
		snapshot := memoryruntime.BuildSnapshot(memoryruntime.Input{Memory: cfg.Memory})
		if err := memoryruntime.WriteSnapshot(memoryruntime.DefaultSnapshotPath, snapshot); err != nil {
			log.Warn("write memory runtime snapshot failed", "path", memoryruntime.DefaultSnapshotPath, "error", err)
		}
	}

	success = true
	return kernel, cancel, nil
}

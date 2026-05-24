package memoryhost

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type Host struct {
	Service memorycore.Service
	Source  string
	DBPath  string
	logger  *slog.Logger
}

func OpenFromConfig(ctx context.Context, path string, logger *slog.Logger) (*Host, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("memorycore config path is required")
	}

	cfg, err := memconfig.LoadEffective(memconfig.LoadEffectiveOptions{
		ConfigPath: path,
	})
	if err != nil {
		return nil, fmt.Errorf("load memorycore config %q: %w", path, err)
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("memorycore config enabled must be true")
	}

	runtime, err := cfg.Runtime()
	if err != nil {
		return nil, fmt.Errorf("build memorycore runtime config: %w", err)
	}
	if !runtime.Options.AutoMigrate {
		return nil, fmt.Errorf("memorycore core.auto_migrate must be true")
	}

	return open(ctx, runtime.Options, logger, path)
}

func OpenWithOptions(ctx context.Context, opts memorycore.Options, logger *slog.Logger) (*Host, error) {
	return open(ctx, opts, logger, "options")
}

func (h *Host) Close() error {
	if h == nil || h.Service == nil {
		return nil
	}
	if err := h.Service.Close(); err != nil {
		return err
	}
	if h.logger != nil {
		h.logger.Info("memorycore stopped", "db_path", h.DBPath)
	}
	h.Service = nil
	return nil
}

func open(ctx context.Context, opts memorycore.Options, logger *slog.Logger, source string) (*Host, error) {
	if strings.TrimSpace(opts.DBPath) == "" {
		return nil, fmt.Errorf("memorycore DBPath is required")
	}
	if !opts.AutoMigrate {
		return nil, fmt.Errorf("memorycore AutoMigrate must be true")
	}

	svc, err := memorycore.Open(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("open memorycore service: %w", err)
	}

	host := &Host{
		Service: svc,
		Source:  source,
		DBPath:  opts.DBPath,
		logger:  logger,
	}
	if logger != nil {
		logger.Info("memorycore opened", "source", source, "db_path", opts.DBPath)
	}
	return host, nil
}

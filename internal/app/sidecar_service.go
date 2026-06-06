package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
)

type SidecarService struct {
	infra      *Infra
	config     *ConfigService
	supervisor *sidecarruntime.Supervisor
}

func (s *SidecarService) Supervisor() *sidecarruntime.Supervisor {
	return s.supervisor
}

func (s *SidecarService) SetSupervisor(supervisor *sidecarruntime.Supervisor) {
	s.supervisor = supervisor
}

func (s *SidecarService) Status(ctx context.Context) (sidecarruntime.Status, error) {
	if s.supervisor != nil {
		return s.supervisor.Status(), nil
	}
	spec, _, err := s.config.BuildSidecarSpec(ctx)
	if err != nil {
		return sidecarruntime.Status{}, err
	}
	state := sidecarruntime.StateStopped
	if !spec.Enabled {
		state = sidecarruntime.StateDisabled
	}
	return sidecarruntime.Status{
		State:   state,
		Managed: spec.Managed,
		URL:     spec.EffectiveURL(),
		Adapter: spec.Adapter,
	}, nil
}

func (s *SidecarService) Start(ctx context.Context) (sidecarruntime.Status, error) {
	if s.supervisor != nil {
		status := s.supervisor.Status()
		if status.State == sidecarruntime.StateHealthy || status.State == sidecarruntime.StateStarting {
			return status, nil
		}
	}
	spec, issues, err := s.config.BuildSidecarSpec(ctx)
	if err != nil {
		return sidecarruntime.Status{}, err
	}
	for _, issue := range issues {
		if issue.Severity == "error" {
			return sidecarruntime.Status{}, fmt.Errorf("%s: %s", issue.Path, issue.Message)
		}
	}
	supervisor := sidecarruntime.NewSupervisor(spec, s.infra.Logger)
	status, err := supervisor.Start(ctx)
	if err != nil {
		return sidecarruntime.Status{}, err
	}
	s.supervisor = supervisor
	return status, nil
}

func (s *SidecarService) Stop(ctx context.Context) (sidecarruntime.Status, error) {
	if s.supervisor == nil {
		return s.Status(ctx)
	}
	err := s.supervisor.Stop(ctx)
	s.supervisor = nil
	status, statusErr := s.Status(ctx)
	if err != nil {
		return status, err
	}
	return status, statusErr
}

func (s *SidecarService) Restart(ctx context.Context) (sidecarruntime.Status, error) {
	if s.supervisor != nil {
		_ = s.supervisor.Stop(ctx)
		s.supervisor = nil
	}
	return s.Start(ctx)
}

func (s *SidecarService) GeneratedConfig(ctx context.Context) (string, error) {
	effective, err := s.config.GetEffective(ctx)
	if err != nil {
		return "", err
	}
	return effective.SidecarGeneratedConfig, nil
}

func (s *SidecarService) Logs(ctx context.Context, maxBytes int) (string, error) {
	cfg := config.DefaultConfig().Memory.Sidecar
	spec, _, err := s.config.BuildSidecarSpec(ctx)
	if err == nil {
		cfg.LogPath = spec.LogPath
	} else if s.infra.Config != nil {
		cfg = s.infra.Config.Memory.Sidecar
	}
	if strings.TrimSpace(cfg.LogPath) == "" {
		return "", nil
	}
	body, err := os.ReadFile(cfg.LogPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = 65536
	}
	if len(body) > maxBytes {
		body = body[len(body)-maxBytes:]
	}
	return string(body), nil
}

func (s *SidecarService) Close(ctx context.Context) error {
	if s.supervisor == nil {
		return nil
	}
	if err := s.supervisor.Stop(ctx); err != nil {
		return fmt.Errorf("stop sidecar: %w", err)
	}
	s.supervisor = nil
	return nil
}

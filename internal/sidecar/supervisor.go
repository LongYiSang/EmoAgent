package sidecar

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type State string

const (
	StateDisabled State = "disabled"
	StateStarting State = "starting"
	StateHealthy  State = "healthy"
	StateDegraded State = "degraded"
	StateStopped  State = "stopped"
)

type Status struct {
	State   State  `json:"state"`
	Managed bool   `json:"managed"`
	URL     string `json:"url"`
	Adapter string `json:"adapter"`
	PID     int    `json:"pid,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Supervisor struct {
	spec   Spec
	logger *slog.Logger
	client *http.Client
	cmd    *exec.Cmd
	log    io.Closer
	status Status
}

func NewSupervisor(spec Spec, logger *slog.Logger) *Supervisor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{
		spec:   spec,
		logger: logger,
		client: &http.Client{Timeout: 500 * time.Millisecond},
		status: Status{State: StateStopped, Managed: spec.Managed, URL: spec.EffectiveURL(), Adapter: spec.Adapter},
	}
}

func (s *Supervisor) Start(ctx context.Context) (Status, error) {
	if !s.spec.Enabled {
		s.status = Status{State: StateDisabled, Managed: s.spec.Managed, URL: s.spec.EffectiveURL(), Adapter: s.spec.Adapter}
		return s.status, nil
	}
	if err := s.spec.Validate(); err != nil {
		return Status{}, err
	}
	if !s.spec.Managed {
		if err := s.checkHealth(ctx); err != nil {
			return s.degradeOrError(err)
		}
		s.status = Status{State: StateHealthy, Managed: false, URL: s.spec.EffectiveURL(), Adapter: s.spec.Adapter}
		return s.status, nil
	}

	if err := s.writeGeneratedConfig(); err != nil {
		return s.degradeOrError(fmt.Errorf("write sidecar generated config: %w", err))
	}
	args := s.spec.CommandArgs()
	if len(args) == 0 {
		return s.degradeOrError(fmt.Errorf("sidecar command is required"))
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if s.spec.WorkingDir != "" {
		cmd.Dir = s.spec.WorkingDir
	}
	logFile, err := s.openLog()
	if err != nil {
		return s.degradeOrError(fmt.Errorf("open sidecar log: %w", err))
	}
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return s.degradeOrError(fmt.Errorf("start sidecar: %w", err))
	}
	s.cmd = cmd
	s.log = logFile
	s.status = Status{State: StateStarting, Managed: true, URL: s.spec.EffectiveURL(), Adapter: s.spec.Adapter, PID: cmd.Process.Pid}
	if err := s.waitHealthy(ctx); err != nil {
		_ = s.Stop(context.Background())
		return s.degradeOrError(err)
	}
	s.status.State = StateHealthy
	return s.status, nil
}

func (s *Supervisor) Stop(ctx context.Context) error {
	var stopErr error
	if s.cmd != nil && s.cmd.Process != nil {
		done := make(chan error, 1)
		go func() {
			done <- s.cmd.Wait()
		}()
		_ = s.cmd.Process.Kill()
		timeout := s.spec.ShutdownTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		select {
		case err := <-done:
			if err != nil {
				stopErr = err
			}
		case <-time.After(timeout):
			stopErr = fmt.Errorf("sidecar shutdown timed out")
		case <-ctx.Done():
			stopErr = ctx.Err()
		}
	}
	if s.log != nil {
		if err := s.log.Close(); err != nil && stopErr == nil {
			stopErr = err
		}
	}
	s.cmd = nil
	s.log = nil
	s.status.State = StateStopped
	return stopErr
}

func (s *Supervisor) Status() Status {
	return s.status
}

func (s *Supervisor) waitHealthy(ctx context.Context) error {
	timeout := s.spec.StartupTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		if err := s.checkHealth(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("sidecar health check timed out: %w", lastErr)
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) checkHealth(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.spec.EffectiveURL()+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sidecar health status %d", resp.StatusCode)
	}
	return nil
}

func (s *Supervisor) writeGeneratedConfig() error {
	body, err := RenderConfig(s.spec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.spec.ConfigPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.spec.ConfigPath, body, 0o600)
}

func (s *Supervisor) openLog() (*os.File, error) {
	path := s.spec.LogPath
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func (s *Supervisor) degradeOrError(err error) (Status, error) {
	if !s.spec.FailOpen {
		return Status{}, err
	}
	s.logger.Warn("sidecar unavailable; continuing with SQLite/FTS fallback", "error", err)
	s.status = Status{State: StateDegraded, Managed: s.spec.Managed, URL: s.spec.EffectiveURL(), Adapter: s.spec.Adapter, Error: err.Error()}
	return s.status, nil
}

package app

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestSidecarServiceConcurrentStartStopStatus(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Sidecar.Enabled = false
	infra := &Infra{
		Config: cfg,
		Logger: slog.Default(),
	}
	service := newServices(infra).Sidecar

	var wg sync.WaitGroup
	errs := make(chan error, 96)
	for i := 0; i < 32; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, err := service.Start(context.Background())
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := service.Stop(context.Background())
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := service.Status(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent sidecar operation: %v", err)
		}
	}
}

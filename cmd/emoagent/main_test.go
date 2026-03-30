package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type fakeApp struct {
	initErr     error
	runErr      error
	shutdownErr error
}

func (a *fakeApp) Init(context.Context, string) error { return a.initErr }
func (a *fakeApp) Run(context.Context) error          { return a.runErr }
func (a *fakeApp) Shutdown() error                    { return a.shutdownErr }

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunReturnsZeroOnSuccess(t *testing.T) {
	code := runApp(context.Background(), &fakeApp{}, "./config.yaml", silentLogger())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRunReturnsOneOnInitError(t *testing.T) {
	code := runApp(context.Background(), &fakeApp{initErr: errors.New("boom")}, "./config.yaml", silentLogger())
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRunReturnsOneOnRuntimeError(t *testing.T) {
	code := runApp(context.Background(), &fakeApp{runErr: errors.New("boom")}, "./config.yaml", silentLogger())
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

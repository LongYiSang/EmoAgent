package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/app"
	"github.com/longyisang/emoagent/internal/plugin"
)

type fakeApp struct {
	initErr     error
	runErr      error
	shutdownErr error
	selfTest    app.SelfTestReport
	selfTestErr error
	ran         bool
	selfTests   int
}

func (a *fakeApp) Init(context.Context, string) error { return a.initErr }
func (a *fakeApp) Run(context.Context) error {
	a.ran = true
	return a.runErr
}
func (a *fakeApp) Shutdown() error { return a.shutdownErr }
func (a *fakeApp) SelfTest(context.Context) (app.SelfTestReport, error) {
	a.selfTests++
	if a.selfTest.Status == "" && a.selfTestErr == nil {
		return app.SelfTestReport{Status: "ok"}, nil
	}
	return a.selfTest, a.selfTestErr
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunReturnsZeroOnSuccess(t *testing.T) {
	code := runApp(context.Background(), &fakeApp{}, "./config.yaml", silentLogger())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRunPerformsStartupSelfTestBeforeHTTPRun(t *testing.T) {
	fake := &fakeApp{selfTest: app.SelfTestReport{Status: "ok"}}
	code := runApp(context.Background(), fake, "./config.yaml", silentLogger())
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if fake.selfTests != 1 {
		t.Fatalf("self-tests = %d, want 1", fake.selfTests)
	}
	if !fake.ran {
		t.Fatal("startup self-test should not skip HTTP runtime")
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

func TestRunSelfTestWritesJSONAndSkipsHTTPRun(t *testing.T) {
	fake := &fakeApp{selfTest: app.SelfTestReport{
		Status: "ok",
		PluginDiagnostics: plugin.AdminPluginDiagnostics{
			Status: "ok",
			Checks: []plugin.AdminPluginDiagnosticCheck{{ID: "process_guard", Status: "ok"}},
		},
	}}
	var out bytes.Buffer
	code := runAppSelfTest(context.Background(), fake, "./config.yaml", silentLogger(), &out, true, true)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if fake.ran {
		t.Fatal("self-test should not start HTTP runtime")
	}
	if fake.selfTests != 1 {
		t.Fatalf("self-tests = %d, want 1", fake.selfTests)
	}
	if !strings.Contains(out.String(), `"status": "ok"`) || !strings.Contains(out.String(), `"process_guard"`) {
		t.Fatalf("self-test json = %s", out.String())
	}
}

func TestRunSelfTestStrictFailsOnWarning(t *testing.T) {
	fake := &fakeApp{selfTest: app.SelfTestReport{
		Status:            "warning",
		PluginDiagnostics: plugin.AdminPluginDiagnostics{Status: "warning"},
	}}
	var out bytes.Buffer
	code := runAppSelfTest(context.Background(), fake, "./config.yaml", silentLogger(), &out, false, true)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "self-test status: warning") {
		t.Fatalf("self-test output = %s", out.String())
	}
}

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/processguard"
)

type PrivatePythonSelfTestResult struct {
	PythonExecutable     string
	Isolated             bool
	SafePath             bool
	SecretEnvSeen        bool
	ProcessGuardKind     string
	ProcessGuardAttached bool
	DurationMS           int64
}

func SelfTestPrivatePythonRuntime(ctx context.Context, pythonExecutable string, timeout time.Duration) (PrivatePythonSelfTestResult, error) {
	started := time.Now()
	path := strings.TrimSpace(pythonExecutable)
	result := PrivatePythonSelfTestResult{PythonExecutable: path}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if path == "" {
		return result, fmt.Errorf("private python executable is required")
	}
	if !filepath.IsAbs(path) {
		return result, fmt.Errorf("private python executable must be an absolute path")
	}
	if info, err := os.Stat(path); err != nil {
		return result, fmt.Errorf("private python executable is not available: %w", err)
	} else if info.IsDir() {
		return result, fmt.Errorf("private python executable is a directory")
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	script := `import json, os, sys, time; time.sleep(0.05); print(json.dumps({"isolated": bool(sys.flags.isolated), "safe_path": bool(getattr(sys.flags, "safe_path", False)), "secret_env_seen": any(token in key.upper() for key in os.environ for token in ("API_KEY", "SECRET", "TOKEN", "PASSWORD"))}))`
	cmd := exec.Command(path, "-I", "-P", "-c", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = []string{}
	allowedEnv := []string{"SystemRoot", "WINDIR", "TEMP", "TMP"}
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok && envNameAllowed(key, allowedEnv) {
			cmd.Env = append(cmd.Env, item)
		}
	}
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start private python self-test: %w", err)
	}
	guard := processguard.New()
	if err := guard.Attach(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		snapshot := guard.Snapshot()
		result.ProcessGuardKind = snapshot.Kind
		result.ProcessGuardAttached = snapshot.Attached
		result.DurationMS = time.Since(started).Milliseconds()
		_ = guard.Close()
		return result, fmt.Errorf("private python self-test process guard attach: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		snapshot := guard.Snapshot()
		result.ProcessGuardKind = snapshot.Kind
		result.ProcessGuardAttached = snapshot.Attached
		result.DurationMS = time.Since(started).Milliseconds()
		_ = guard.Close()
		if err != nil {
			return result, fmt.Errorf("private python self-test exited: %w", err)
		}
	case <-runCtx.Done():
		_ = guard.Terminate(1)
		_ = cmd.Process.Kill()
		<-done
		snapshot := guard.Snapshot()
		result.ProcessGuardKind = snapshot.Kind
		result.ProcessGuardAttached = snapshot.Attached
		result.DurationMS = time.Since(started).Milliseconds()
		_ = guard.Close()
		return result, fmt.Errorf("private python self-test timed out: %w", runCtx.Err())
	}

	var payload struct {
		Isolated      bool `json:"isolated"`
		SafePath      bool `json:"safe_path"`
		SecretEnvSeen bool `json:"secret_env_seen"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &payload); err != nil {
		return result, fmt.Errorf("private python self-test returned invalid output")
	}
	result.Isolated = payload.Isolated
	result.SafePath = payload.SafePath
	result.SecretEnvSeen = payload.SecretEnvSeen
	if result.SecretEnvSeen {
		return result, fmt.Errorf("private python self-test inherited sensitive environment")
	}
	if !result.Isolated || !result.SafePath {
		return result, fmt.Errorf("private python self-test did not run with isolated safe path")
	}
	return result, nil
}

func envNameAllowed(name string, allowed []string) bool {
	for _, item := range allowed {
		if strings.EqualFold(name, item) {
			return true
		}
	}
	return false
}

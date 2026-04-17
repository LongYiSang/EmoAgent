package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func defaultBashCfg() config.BashConfig {
	return config.BashConfig{
		Enabled:        true,
		TimeoutSec:     10,
		MaxOutputBytes: 1024,
	}
}

func TestBash_Echo(t *testing.T) {
	root := t.TempDir()
	_, handler := NewBashTool(defaultBashCfg(), root, nil)

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "echo hello"
	} else {
		cmd = "echo hello"
	}

	input, _ := json.Marshal(map[string]string{"command": cmd})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Stdout   string `json:"stdout"`
		ExitCode int    `json:"exit_code"`
		TimedOut bool   `json:"timed_out"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(out.Stdout, "hello") {
		t.Fatalf("stdout = %q, want 'hello'", out.Stdout)
	}
	if out.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", out.ExitCode)
	}
	if out.TimedOut {
		t.Fatal("timed_out should be false")
	}
}

func TestBash_NonZeroExitNotAnError(t *testing.T) {
	root := t.TempDir()
	_, handler := NewBashTool(defaultBashCfg(), root, nil)

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "exit 1"
	} else {
		cmd = "exit 1"
	}

	input, _ := json.Marshal(map[string]string{"command": cmd})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler must not error on non-zero exit: %v", err)
	}

	var out struct {
		ExitCode int `json:"exit_code"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ExitCode == 0 {
		t.Fatal("exit_code should be non-zero")
	}
}

func TestBash_StdoutTruncation(t *testing.T) {
	root := t.TempDir()
	cfg := defaultBashCfg()
	cfg.MaxOutputBytes = 10
	_, handler := NewBashTool(cfg, root, nil)

	var cmd string
	if runtime.GOOS == "windows" {
		// Print >10 chars on Windows
		cmd = "echo abcdefghijklmnopqrstuvwxyz"
	} else {
		cmd = "printf '%0.s-' {1..100}"
	}

	input, _ := json.Marshal(map[string]string{"command": cmd})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		StdoutTruncated bool `json:"stdout_truncated"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.StdoutTruncated {
		t.Fatal("stdout_truncated should be true")
	}
}

func TestBash_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	root := t.TempDir()
	cfg := defaultBashCfg()
	cfg.TimeoutSec = 1
	_, handler := NewBashTool(cfg, root, nil)

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "ping -n 10 127.0.0.1"
	} else {
		cmd = "sleep 10"
	}

	input, _ := json.Marshal(map[string]string{"command": cmd})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler must not error on timeout: %v", err)
	}

	var out struct {
		TimedOut bool `json:"timed_out"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.TimedOut {
		t.Fatal("timed_out should be true")
	}
}

func TestBash_EmptyCommand(t *testing.T) {
	root := t.TempDir()
	_, handler := NewBashTool(defaultBashCfg(), root, nil)
	input, _ := json.Marshal(map[string]string{"command": ""})
	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestResolveShell_Override(t *testing.T) {
	args := resolveShell("/bin/bash")
	if len(args) != 2 || args[0] != "/bin/bash" || args[1] != "-c" {
		t.Fatalf("resolveShell = %v, want [/bin/bash -c]", args)
	}
}

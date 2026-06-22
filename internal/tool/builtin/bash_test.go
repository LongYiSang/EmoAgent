package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/runtimeenv"
)

func defaultBashCfg() config.BashConfig {
	return config.BashConfig{
		Enabled:        true,
		TimeoutSec:     10,
		MaxOutputBytes: 1024,
		ExecutionMode:  "managed_host",
	}
}

func unsafeBashCfg() config.BashConfig {
	cfg := defaultBashCfg()
	cfg.ExecutionMode = "legacy_host"
	cfg.UnsafeHostExecEnabled = true
	return cfg
}

func TestBash_DefaultManagedHostRunsAndIsNotSandboxed(t *testing.T) {
	root := t.TempDir()
	cfg := defaultBashCfg()
	_, handler := NewBashTool(cfg, root, nil)

	raw, err := handler(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var out struct {
		Stdout            string `json:"stdout"`
		Unavailable       bool   `json:"unavailable"`
		UnavailableReason string `json:"unavailable_reason"`
		ExecutionMode     string `json:"execution_mode"`
		IsolationLevel    string `json:"isolation_level"`
		Sandboxed         bool   `json:"sandboxed"`
		Unsafe            bool   `json:"unsafe"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Unavailable || out.UnavailableReason != "" {
		t.Fatalf("out = %#v, want available managed host process", out)
	}
	if !strings.Contains(out.Stdout, "hello") {
		t.Fatalf("stdout = %q, want hello", out.Stdout)
	}
	if out.ExecutionMode != "managed_host" || out.IsolationLevel != "current_user_process" || out.Sandboxed || out.Unsafe {
		t.Fatalf("managed host labels = %#v", out)
	}
}

func TestBash_UnsafeHostEchoIsExplicitlyLabeled(t *testing.T) {
	root := t.TempDir()
	_, handler := NewBashTool(unsafeBashCfg(), root, nil)

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
		Stdout        string `json:"stdout"`
		ExitCode      int    `json:"exit_code"`
		TimedOut      bool   `json:"timed_out"`
		Unsafe        bool   `json:"unsafe"`
		ExecutionMode string `json:"execution_mode"`
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
	if !out.Unsafe || out.ExecutionMode != "unsafe_host_exec" {
		t.Fatalf("unsafe labels = unsafe:%v execution_mode:%q", out.Unsafe, out.ExecutionMode)
	}
}

func TestBash_ExplicitSandboxModeUnavailableDoesNotExecuteHostCommand(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "should-not-exist.txt")
	cfg := defaultBashCfg()
	cfg.ExecutionMode = "sandbox"
	_, handler := NewBashTool(cfg, root, nil)

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "echo ran > should-not-exist.txt"
	} else {
		cmd = "touch should-not-exist.txt"
	}
	raw, err := handler(context.Background(), mustJSON(t, map[string]string{"command": cmd}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var out struct {
		Unavailable       bool   `json:"unavailable"`
		UnavailableReason string `json:"unavailable_reason"`
		ExecutionMode     string `json:"execution_mode"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Unavailable || out.ExecutionMode != "sandbox" || out.UnavailableReason == "" {
		t.Fatalf("sandbox result = %#v, want explicit unavailable", out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("sandbox fallback executed host command; marker stat err=%v", err)
	}
}

func TestBash_NonZeroExitNotAnError(t *testing.T) {
	root := t.TempDir()
	_, handler := NewBashTool(unsafeBashCfg(), root, nil)

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
	cfg := unsafeBashCfg()
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

func TestCappedBufferWriteReturnsOriginalLengthWhenTruncated(t *testing.T) {
	var buf cappedBuffer
	buf.cap = 3

	n, err := buf.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != len("hello") {
		t.Fatalf("Write returned %d, want %d", n, len("hello"))
	}
	if got := buf.String(); got != "hel" {
		t.Fatalf("buffer = %q, want hel", got)
	}
	if !buf.truncated {
		t.Fatal("truncated should be true")
	}
}

func TestBash_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	root := t.TempDir()
	cfg := unsafeBashCfg()
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

func TestNewBashTool_WindowsDescriptionIncludesShellAndCommandHints(t *testing.T) {
	spec, _ := NewBashToolWithFacts(defaultBashCfg(), runtimeenv.Facts{
		OS:              "windows",
		WorkspaceRoot:   `D:\repo`,
		PathStyle:       "windows",
		BashEnabled:     true,
		ShellDisplay:    "cmd /c",
		ShellExecutable: "cmd",
		ShellArgsPrefix: []string{"/c"},
	}, nil)

	for _, snippet := range []string{
		"Windows",
		"cmd /c",
		`workspace root D:\repo`,
		"Do not assume Unix commands such as ls, rm, or pwd are available.",
		"Prefer read_file, list_dir, write_file, and edit_file",
		"Use for tests, builds, command-based verification",
		"Non-zero exit codes are not errors",
	} {
		if !strings.Contains(spec.Description, snippet) {
			t.Fatalf("description missing %q: %s", snippet, spec.Description)
		}
	}
}

func TestNewBashTool_AttachesDestructiveClassifier(t *testing.T) {
	spec, _ := NewBashTool(defaultBashCfg(), t.TempDir(), nil)
	if spec.DestructiveClassifier == nil {
		t.Fatal("bash spec should include a destructive classifier")
	}

	tests := []struct {
		name  string
		input json.RawMessage
		want  bool
	}{
		{name: "rm", input: json.RawMessage(`{"command":"rm -rf tmp"}`), want: true},
		{name: "powershell remove-item", input: json.RawMessage(`{"command":"Remove-Item -Recurse tmp"}`), want: true},
		{name: "git reset hard", input: json.RawMessage(`{"command":"git reset --hard HEAD~1"}`), want: true},
		{name: "echo", input: json.RawMessage(`{"command":"echo hello"}`), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := spec.DestructiveClassifier(tt.input)
			if got != tt.want {
				t.Fatalf("DestructiveClassifier(%s) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewBashTool_SourceMetadataReflectsExecutionMode(t *testing.T) {
	managedSpec, _ := NewBashTool(defaultBashCfg(), t.TempDir(), nil)
	if managedSpec.Source.RuntimeKind != "managed_host_process" || managedSpec.Source.DefaultLabels.Integrity != "host_verified" {
		t.Fatalf("managed host source = %#v", managedSpec.Source)
	}

	unsafeSpec, _ := NewBashTool(unsafeBashCfg(), t.TempDir(), nil)
	if unsafeSpec.Source.RuntimeKind != "host" || unsafeSpec.Source.DefaultLabels.Integrity != "unverified" {
		t.Fatalf("unsafe source = %#v", unsafeSpec.Source)
	}
}

func TestBashManagedProfileCarriesConfiguredProcessLimits(t *testing.T) {
	cfg := defaultBashCfg()
	cfg.MaxOutputBytes = 4096
	cfg.MaxProcesses = 8
	cfg.MemoryMB = 128

	profile := bashManagedProcessProfile(cfg, 7)
	if profile.WorkspaceMode != "rw" || profile.TempMode != "rw" || profile.PersonalMode != "ro" {
		t.Fatalf("profile modes = %#v", profile)
	}
	if string(profile.NetworkMode) != "allow" {
		t.Fatalf("network mode = %q, want allow for current-user managed host process", profile.NetworkMode)
	}
	if len(profile.EnvAllowlist) == 0 {
		t.Fatal("EnvAllowlist should not be empty")
	}
	if profile.Limits.TimeoutSeconds != 7 ||
		profile.Limits.MaxOutputBytes != 4096 ||
		profile.Limits.MaxProcesses != 8 ||
		profile.Limits.MemoryBytes != 128<<20 {
		t.Fatalf("limits = %#v", profile.Limits)
	}
}

func TestBashHandlerDoesNotImportOrCallHostExecDirectly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("bash.go"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	source := string(data)
	for _, forbidden := range []string{`"os/exec"`, "exec.CommandContext", "exec.Command("} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("bash.go contains forbidden direct host exec token %q", forbidden)
		}
	}
}

func TestResolveShellArgs_Override(t *testing.T) {
	args := resolveShellArgs(runtimeenv.ShellSpec{
		Executable: "/bin/bash",
		ArgsPrefix: []string{"-c"},
	})
	if len(args) != 2 || args[0] != "/bin/bash" || args[1] != "-c" {
		t.Fatalf("resolveShellArgs = %v, want [/bin/bash -c]", args)
	}
}

package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/tool"
)

const bashMaxTimeoutSec = 300

// NewBashTool constructs the bash tool for Work.
func NewBashTool(cfg config.BashConfig, projectRoot string, logger *slog.Logger) (tool.Spec, tool.Handler) {
	facts := runtimeenv.BuildEnvironmentFacts(runtime.GOOS, projectRoot, cfg)
	return NewBashToolWithFacts(cfg, facts, logger)
}

// NewBashToolWithFacts constructs the bash tool for Work using explicit environment facts.
func NewBashToolWithFacts(cfg config.BashConfig, facts runtimeenv.Facts, logger *slog.Logger) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "bash",
		Description: buildBashDescription(facts),
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"command":{"type":"string"},
				"timeout_sec":{"type":"integer"}
			},
			"required":["command"],
			"additionalProperties":false
		}`),
		Scope:                 tool.ScopeWork,
		Permission:            tool.PermWorkspaceWrite,
		DestructiveClassifier: classifyBashDestructive,
	}

	shellArgs := resolveShellArgs(runtimeenv.ShellSpec{
		Executable: facts.ShellExecutable,
		ArgsPrefix: facts.ShellArgsPrefix,
	})

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Command    string `json:"command"`
			TimeoutSec int    `json:"timeout_sec"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("bash: invalid input: %w", err)
		}
		if in.Command == "" {
			return nil, fmt.Errorf("bash: command is required")
		}

		timeoutSec := in.TimeoutSec
		if timeoutSec <= 0 {
			timeoutSec = cfg.TimeoutSec
		}
		if timeoutSec > bashMaxTimeoutSec {
			timeoutSec = bashMaxTimeoutSec
		}

		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()

		args := append(shellArgs, in.Command)
		cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
		cmd.Dir = facts.WorkspaceRoot

		cap := cfg.MaxOutputBytes
		var stdoutBuf, stderrBuf cappedBuffer
		stdoutBuf.cap = cap
		stderrBuf.cap = cap
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf

		runErr := cmd.Run()

		timedOut := runCtx.Err() == context.DeadlineExceeded
		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		} else if runErr != nil && !timedOut {
			return nil, fmt.Errorf("bash: start failed: %w", runErr)
		}

		if logger != nil {
			logger.DebugContext(ctx, "bash done",
				"exit_code", exitCode,
				"timed_out", timedOut,
				"stdout_bytes", stdoutBuf.written,
			)
		}

		return json.Marshal(map[string]any{
			"stdout":           stdoutBuf.String(),
			"stdout_truncated": stdoutBuf.truncated,
			"stderr":           stderrBuf.String(),
			"stderr_truncated": stderrBuf.truncated,
			"exit_code":        exitCode,
			"timed_out":        timedOut,
		})
	}

	return spec, handler
}

func buildBashDescription(facts runtimeenv.Facts) string {
	var b strings.Builder
	b.WriteString("Run a shell command from the workspace root ")
	if facts.WorkspaceRoot != "" {
		b.WriteString(facts.WorkspaceRoot)
	} else {
		b.WriteString(".")
	}
	b.WriteString(". ")
	b.WriteString("Execution environment: ")
	b.WriteString(facts.DisplayOS())
	if facts.ShellDisplay != "" {
		b.WriteString(" via ")
		b.WriteString(facts.ShellDisplay)
	}
	b.WriteString(". ")
	if strings.EqualFold(facts.OS, "windows") {
		b.WriteString("Do not assume Unix commands such as ls, rm, or pwd are available. ")
	}
	b.WriteString("Prefer read_file, list_dir, write_file, and edit_file for file operations. ")
	b.WriteString("Returns stdout, stderr, exit code, and whether the process timed out. ")
	b.WriteString("Non-zero exit codes are not errors — inspect the output to determine success.")
	return b.String()
}

func classifyBashDestructive(input json.RawMessage) (bool, string) {
	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return false, ""
	}
	command := strings.ToLower(" " + payload.Command + " ")
	if command == "  " {
		return false, ""
	}
	for _, needle := range []string{
		" git reset --hard",
		" git clean -",
		" git checkout --",
		" git restore --source",
		" remove-item ",
		" del ",
		" erase ",
		" rmdir ",
		" rd ",
		" rm ",
		" rm -",
		" cp -f ",
		" mv -f ",
		" copy /y ",
		" move /y ",
		" truncate ",
	} {
		if strings.Contains(command, needle) {
			return true, "bash command may perform destructive file or git operations"
		}
	}
	return false, ""
}

// resolveShellArgs returns the full shell invocation prefix.
func resolveShellArgs(spec runtimeenv.ShellSpec) []string {
	args := []string{spec.Executable}
	args = append(args, spec.ArgsPrefix...)
	return args
}

// cappedBuffer is an io.Writer that accepts at most cap bytes and marks itself truncated beyond that.
type cappedBuffer struct {
	buf       bytes.Buffer
	cap       int
	written   int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.cap - c.written
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil // discard
	}
	if len(p) > remaining {
		c.truncated = true
		p = p[:remaining]
	}
	n, err := c.buf.Write(p)
	c.written += n
	return len(p), err // report full p consumed even if we truncated
}

func (c *cappedBuffer) String() string {
	return c.buf.String()
}

var _ io.Writer = (*cappedBuffer)(nil)

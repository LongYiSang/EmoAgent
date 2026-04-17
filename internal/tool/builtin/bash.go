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
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool"
)

const bashMaxTimeoutSec = 300

// NewBashTool constructs the bash tool for Work.
func NewBashTool(cfg config.BashConfig, projectRoot string, logger *slog.Logger) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "bash",
		Description: "Run a shell command in the workspace directory. Returns stdout, stderr, exit code, and whether the process timed out. Non-zero exit codes are not errors — inspect the output to determine success.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"command":{"type":"string"},
				"timeout_sec":{"type":"integer"}
			},
			"required":["command"],
			"additionalProperties":false
		}`),
		Scope:      tool.ScopeWork,
		Permission: tool.PermWorkspaceWrite,
	}

	shellArgs := resolveShell(cfg.Shell)

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
		cmd.Dir = projectRoot

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

// resolveShell returns the shell invocation prefix (e.g. ["cmd", "/c"] or ["sh", "-c"]).
func resolveShell(override string) []string {
	if override != "" {
		return []string{override, "-c"}
	}
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c"}
	}
	return []string{"sh", "-c"}
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

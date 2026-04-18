package runtimeenv

import (
	"path/filepath"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

// ShellSpec describes the actual shell invocation used by the bash tool.
type ShellSpec struct {
	Executable string
	ArgsPrefix []string
	Display    string
}

// Facts captures the execution environment visible to agents and tools.
type Facts struct {
	OS              string
	WorkspaceRoot   string
	PathStyle       string
	BashEnabled     bool
	ShellDisplay    string
	ShellExecutable string
	ShellArgsPrefix []string
}

// ResolveShellSpec returns the shell invocation for the given OS and override.
func ResolveShellSpec(goos, shellOverride string) ShellSpec {
	if shellOverride != "" {
		return ShellSpec{
			Executable: shellOverride,
			ArgsPrefix: []string{"-c"},
			Display:    shellOverride + " -c",
		}
	}
	if strings.EqualFold(goos, "windows") {
		return ShellSpec{
			Executable: "cmd",
			ArgsPrefix: []string{"/c"},
			Display:    "cmd /c",
		}
	}
	return ShellSpec{
		Executable: "sh",
		ArgsPrefix: []string{"-c"},
		Display:    "sh -c",
	}
}

// BuildEnvironmentFacts derives the normalized runtime environment for prompts and tools.
func BuildEnvironmentFacts(goos, workspaceRoot string, bashCfg config.BashConfig) Facts {
	facts := Facts{
		OS:            strings.ToLower(goos),
		WorkspaceRoot: workspaceRoot,
		BashEnabled:   bashCfg.Enabled,
	}
	if strings.EqualFold(goos, "windows") {
		facts.PathStyle = "windows"
	} else {
		facts.PathStyle = "posix"
	}
	if facts.WorkspaceRoot != "" {
		facts.WorkspaceRoot = filepath.Clean(facts.WorkspaceRoot)
	}
	if !facts.BashEnabled {
		return facts
	}

	shell := ResolveShellSpec(goos, bashCfg.Shell)
	facts.ShellDisplay = shell.Display
	facts.ShellExecutable = shell.Executable
	facts.ShellArgsPrefix = append([]string(nil), shell.ArgsPrefix...)
	return facts
}

// DisplayOS returns a short user-facing label for the current OS.
func (f Facts) DisplayOS() string {
	switch strings.ToLower(f.OS) {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		if f.OS == "" {
			return "Unknown"
		}
		return f.OS
	}
}

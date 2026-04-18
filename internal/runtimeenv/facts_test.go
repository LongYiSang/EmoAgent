package runtimeenv

import (
	"reflect"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestResolveShellSpec_DefaultWindows(t *testing.T) {
	spec := ResolveShellSpec("windows", "")
	if spec.Executable != "cmd" {
		t.Fatalf("Executable = %q, want cmd", spec.Executable)
	}
	if !reflect.DeepEqual(spec.ArgsPrefix, []string{"/c"}) {
		t.Fatalf("ArgsPrefix = %#v, want [/c]", spec.ArgsPrefix)
	}
	if spec.Display != "cmd /c" {
		t.Fatalf("Display = %q, want cmd /c", spec.Display)
	}
}

func TestResolveShellSpec_DefaultPOSIX(t *testing.T) {
	spec := ResolveShellSpec("linux", "")
	if spec.Executable != "sh" {
		t.Fatalf("Executable = %q, want sh", spec.Executable)
	}
	if !reflect.DeepEqual(spec.ArgsPrefix, []string{"-c"}) {
		t.Fatalf("ArgsPrefix = %#v, want [-c]", spec.ArgsPrefix)
	}
	if spec.Display != "sh -c" {
		t.Fatalf("Display = %q, want sh -c", spec.Display)
	}
}

func TestResolveShellSpec_Override(t *testing.T) {
	spec := ResolveShellSpec("windows", "powershell")
	if spec.Executable != "powershell" {
		t.Fatalf("Executable = %q, want powershell", spec.Executable)
	}
	if !reflect.DeepEqual(spec.ArgsPrefix, []string{"-c"}) {
		t.Fatalf("ArgsPrefix = %#v, want [-c]", spec.ArgsPrefix)
	}
	if spec.Display != "powershell -c" {
		t.Fatalf("Display = %q, want powershell -c", spec.Display)
	}
}

func TestBuildEnvironmentFacts_DisabledBashOmitsShell(t *testing.T) {
	facts := BuildEnvironmentFacts("windows", `D:\repo`, config.BashConfig{})
	if facts.OS != "windows" {
		t.Fatalf("OS = %q, want windows", facts.OS)
	}
	if facts.PathStyle != "windows" {
		t.Fatalf("PathStyle = %q, want windows", facts.PathStyle)
	}
	if facts.BashEnabled {
		t.Fatal("BashEnabled = true, want false")
	}
	if facts.ShellDisplay != "" {
		t.Fatalf("ShellDisplay = %q, want empty", facts.ShellDisplay)
	}
}

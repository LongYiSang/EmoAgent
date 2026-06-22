package execution

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

type compileExecutor struct{}

func (compileExecutor) Probe(context.Context, ProbeRequest) (ProbeResult, error) {
	return ProbeResult{}, nil
}

func (compileExecutor) Execute(context.Context, CommandRequest) (CommandResult, error) {
	return CommandResult{}, nil
}

func (compileExecutor) Cancel(context.Context, string) error {
	return nil
}

var _ CommandExecutor = compileExecutor{}

func TestCommandExecutorContractCompile(t *testing.T) {
	req := CommandRequest{
		Command:      []string{"go", "test", "./..."},
		WorkspaceDir: "/workspace",
		Profile: ManagedProcessProfile{
			WorkspaceMode: "rw",
			NetworkMode:   NetworkDeny,
			EnvAllowlist:  []string{"PATH"},
			Limits: CommandLimits{
				TimeoutSeconds: 60,
				MaxProcesses:   64,
				MaxOutputBytes: 1024,
			},
		},
		Metadata: json.RawMessage(`{"tool":"bash"}`),
	}
	if req.Profile.NetworkMode != NetworkDeny || req.Profile.Limits.MaxProcesses != 64 {
		t.Fatalf("command executor contract mismatch: %#v", req)
	}
}

func TestExecutionPackageDoesNotExportLegacySandboxExecutorTypes(t *testing.T) {
	forbidden := map[string]struct{}{
		"CommandSandbox":     {},
		"SandboxProfile":     {},
		"UnavailableSandbox": {},
		"UnsafeHostSandbox":  {},
	}
	files := []string{"types.go", "manager.go"}
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				if _, ok := forbidden[typeSpec.Name.Name]; ok {
					t.Fatalf("%s still declares legacy execution type %s", file, typeSpec.Name.Name)
				}
			}
		}
	}
}

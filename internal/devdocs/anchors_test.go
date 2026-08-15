package devdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractAnchors(t *testing.T) {
	md := "见 `internal/tool/spec.go:38-45` 与 `docs/dev/README.md`。\n" +
		"接口 `/api/plugins/{id}/logs` 与 `emoagent.plugin.v0.2` 不是锚点。\n" +
		"外链 `https://example.com/a/b` 也不是。\n"

	got := ExtractAnchors(md)

	want := []string{"internal/tool/spec.go", "docs/dev/README.md"}
	if len(got) != len(want) {
		t.Fatalf("ExtractAnchors() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("anchor %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDevDocAnchorsExist is the guard: every repo path referenced from
// docs/dev/ must still resolve. A rename that orphans a doc fails here and
// names the offending file, instead of silently misleading the next reader.
func TestDevDocAnchorsExist(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	devDir := filepath.Join(repoRoot, "docs", "dev")

	err := filepath.Walk(devDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoRoot, path)
		for _, anchor := range ExtractAnchors(string(content)) {
			target := filepath.Join(repoRoot, filepath.FromSlash(anchor))
			if _, statErr := os.Stat(target); statErr != nil {
				t.Errorf("%s references %q, which no longer exists", rel, anchor)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/dev: %v", err)
	}
}

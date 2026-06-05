package web

import (
	"os"
	"strings"
	"testing"
)

func TestAdminHTMLContainsProviderPresetControls(t *testing.T) {
	raw, err := os.ReadFile("static/admin.html")
	if err != nil {
		t.Fatalf("read admin.html: %v", err)
	}
	html := string(raw)
	for _, want := range []string{
		`id="p-preset"`,
		`loadProviderPresets`,
		`applyProviderPreset`,
		`renderSlotCapabilities`,
		`applyRecommendedParams`,
		`Apply recommended`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("admin.html missing %q", want)
		}
	}
}

func TestAdminHTMLContainsConfigCenterControls(t *testing.T) {
	raw, err := os.ReadFile("static/admin.html")
	if err != nil {
		t.Fatalf("read admin.html: %v", err)
	}
	html := string(raw)
	for _, want := range []string{
		`id="p-cap-chat"`,
		`id="p-cap-embedding"`,
		`id="p-cap-rerank"`,
		`id="provider-env-status"`,
		`loadProviderEnvStatus`,
		`data-tab="memory-core"`,
		`data-tab="pipelines"`,
		`data-tab="retrieval-mirror"`,
		`data-tab="sidecar"`,
		`data-tab="privacy-forget"`,
		`data-tab="retention"`,
		`data-tab="diagnostics"`,
		`loadEffectiveConfig`,
		`effective-config-json`,
		`config-issues-list`,
		`sidecar-generated-config`,
		`save-memory-core`,
		`save-pipelines`,
		`save-retrieval`,
		`save-sidecar-config`,
		`save-privacy-forget`,
		`save-retention`,
		`llmPipelineKeys`,
		`pipelineProviderOptions`,
		`pipelineThinkingOptions`,
		`mem-${key}-thinking`,
		`/api/memory/config`,
		`/api/sidecar/start`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("admin.html missing %q", want)
		}
	}
}

func TestAdminHTMLContainsNaturalMemoryControls(t *testing.T) {
	raw, err := os.ReadFile("static/admin.html")
	if err != nil {
		t.Fatalf("read admin.html: %v", err)
	}
	html := string(raw)
	for _, want := range []string{
		`id="natural-memory-enabled"`,
		`id="natural-memory-scheduler"`,
		`id="natural-memory-local-time"`,
		`id="natural-memory-dry-run"`,
		`id="natural-memory-run-now"`,
		`id="natural-memory-latest-status"`,
		`renderNaturalMemory`,
		`loadNaturalMemoryLatest`,
		`runNaturalMemory`,
		`/api/memory/natural-runs/latest`,
		`/api/memory/natural-runs`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("admin.html missing %q", want)
		}
	}
}

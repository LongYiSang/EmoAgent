package web

import (
	"os"
	"strings"
	"testing"
)

func TestIndexStaticIncludesMemoryExtractionControls(t *testing.T) {
	data, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	html := string(data)
	for _, snippet := range []string{
		`id="memory-scan"`,
		`id="memory-status-panel"`,
		`queueMemoryExtraction`,
		`/api/memory/extractions`,
		`/api/memory/segments?session_id=`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("index.html missing %q", snippet)
		}
	}
}

func TestIndexStaticIncludesMemoryPipelineDebugPanel(t *testing.T) {
	data, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	html := string(data)
	for _, snippet := range []string{
		`id="memory-pipeline-panel"`,
		`renderMemoryPipelineButton`,
		`openMemoryPipelinePanel`,
		`renderMemoryPipelinePanel`,
		`metadata?.memory_pipeline`,
		`loadSessionDetail(currentSessionId); renderHistory(d.messages || [])`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("index.html missing %q", snippet)
		}
	}
}

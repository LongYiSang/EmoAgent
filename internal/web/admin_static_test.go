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

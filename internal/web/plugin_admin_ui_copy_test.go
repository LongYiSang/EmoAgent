package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginAdminCopyFramesProcessGuardAsLifecycleNotSandbox(t *testing.T) {
	path := filepath.Join("..", "..", "web", "src", "admin", "tabs", "PluginsTab.tsx")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read PluginsTab: %v", err)
	}
	text := string(raw)

	for _, want := range []string{"生命周期守护", "不是恶意插件沙箱", "不是 OS 沙箱", "用户 Grant", "宿主策略", "Host 派生 Trust", "Trust Acceptance", "Acceptance Hash", "签名不是 OS 沙箱", "Host API", "Tool Policy", "Hook Policy", "active hook 默认关闭", "work + ask", "data-only", "依赖数量", "依赖来源", "Dependency Lock"} {
		if !strings.Contains(text, want) {
			t.Fatalf("PluginsTab missing %q", want)
		}
	}
	if strings.Contains(text, "按插件声明的层级限制") {
		t.Fatalf("PluginsTab still frames permissions as plugin-declared tier authority")
	}
}

func TestPluginAdminUsesGrantPreviewBeforeEnable(t *testing.T) {
	hookPath := filepath.Join("..", "..", "web", "src", "admin", "hooks", "usePluginAdmin.ts")
	hookRaw, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read usePluginAdmin: %v", err)
	}
	hookText := string(hookRaw)
	for _, want := range []string{
		"trustReviewGrantJSON === grantJSON",
		"loadPlugin(selectedPluginID, version, grantJSON)",
		"请确认本次插件信任/策略变更后再次点击启用",
	} {
		if !strings.Contains(hookText, want) {
			t.Fatalf("usePluginAdmin missing %q", want)
		}
	}

	apiPath := filepath.Join("..", "..", "web", "src", "admin", "protocol", "pluginApi.ts")
	apiRaw, err := os.ReadFile(apiPath)
	if err != nil {
		t.Fatalf("read pluginApi: %v", err)
	}
	apiText := string(apiRaw)
	for _, want := range []string{
		"user_grant_json",
		"target_user_grant_hash",
		"dependency_lock_digest",
		"dependency_summary",
		"new URLSearchParams",
	} {
		if !strings.Contains(apiText, want) {
			t.Fatalf("pluginApi missing %q", want)
		}
	}
}

func TestDiagnosticsTabShowsPluginRuntimeDiagnostics(t *testing.T) {
	tabPath := filepath.Join("..", "..", "web", "src", "admin", "tabs", "DiagnosticsTab.tsx")
	tabRaw, err := os.ReadFile(tabPath)
	if err != nil {
		t.Fatalf("read DiagnosticsTab: %v", err)
	}
	tabText := string(tabRaw)
	for _, want := range []string{"Plugin Runtime", "Private Python", "Self-test", "Dependency install", "Job Object", "plugin logs", "repair", "非安全沙箱"} {
		if !strings.Contains(tabText, want) {
			t.Fatalf("DiagnosticsTab missing %q", want)
		}
	}

	apiPath := filepath.Join("..", "..", "web", "src", "admin", "protocol", "adminApi.ts")
	apiRaw, err := os.ReadFile(apiPath)
	if err != nil {
		t.Fatalf("read adminApi: %v", err)
	}
	if !strings.Contains(string(apiRaw), "/api/plugins/diagnostics") {
		t.Fatalf("adminApi missing plugin diagnostics endpoint")
	}
}

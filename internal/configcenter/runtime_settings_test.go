package configcenter

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestApplyRuntimeSettingsSupportsMemoryDetailNamespaces(t *testing.T) {
	seed := config.DefaultConfig()

	effective, issues := ApplyRuntimeSettings(seed, []storage.RuntimeSetting{
		{Namespace: "memory.retention", Key: "config", ValueJSON: `{"thresholds":{"deep_archive_after_days":77}}`},
		{Namespace: "memory.forgetting_privacy", Key: "config", ValueJSON: `{"cleanup":{"verify_after_delete":false}}`},
		{Namespace: "memory.agent_affect", Key: "config", ValueJSON: `{"retrieval":{"weight_cap":0.02}}`},
		{Namespace: "memory.natural_memory", Key: "config", ValueJSON: `{"enabled":true,"local_time":"04:10","manual":{"allow_force":false}}`},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
	if effective.Memory.Retention == nil || effective.Memory.Retention.Thresholds.DeepArchiveAfterDays != 77 {
		t.Fatalf("retention = %#v", effective.Memory.Retention)
	}
	if effective.Memory.ForgettingPrivacy == nil || effective.Memory.ForgettingPrivacy.Cleanup.VerifyAfterDelete {
		t.Fatalf("forgetting_privacy = %#v", effective.Memory.ForgettingPrivacy)
	}
	if effective.Memory.AgentAffect == nil || effective.Memory.AgentAffect.Retrieval.WeightCap != 0.02 {
		t.Fatalf("agent_affect = %#v", effective.Memory.AgentAffect)
	}
	if !effective.Memory.NaturalMemory.Enabled || effective.Memory.NaturalMemory.LocalTime != "04:10" || effective.Memory.NaturalMemory.Manual.AllowForce {
		t.Fatalf("natural_memory = %#v", effective.Memory.NaturalMemory)
	}
}

func TestApplyRuntimeSettingsSupportsRootAgentAffect(t *testing.T) {
	seed := config.DefaultConfig()

	effective, issues := ApplyRuntimeSettings(seed, []storage.RuntimeSetting{
		{Namespace: "agent_affect", Key: "config", ValueJSON: `{"enabled":true,"evaluator":{"mode":"disabled"},"context":{"store_raw_inputs":false},"limits":{"per_request_delta":{"valence":0.05}}}`},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
	if !effective.AgentAffect.Enabled {
		t.Fatalf("agent_affect.enabled = false, want true")
	}
	if effective.AgentAffect.Evaluator.Mode != "disabled" {
		t.Fatalf("agent_affect evaluator mode = %q, want disabled", effective.AgentAffect.Evaluator.Mode)
	}
	if effective.AgentAffect.Context.StoreRawInputs {
		t.Fatalf("agent_affect context store_raw_inputs = true, want false")
	}
	if effective.AgentAffect.Limits.PerRequestDelta.Valence != 0.05 {
		t.Fatalf("agent_affect valence delta = %v, want 0.05", effective.AgentAffect.Limits.PerRequestDelta.Valence)
	}
	if effective.Memory.AgentAffect != nil {
		t.Fatalf("root agent_affect runtime setting mutated memory.agent_affect: %#v", effective.Memory.AgentAffect)
	}
}

func TestApplyRuntimeSettingsSupportsPythonToolchain(t *testing.T) {
	seed := config.DefaultConfig()

	effective, issues := ApplyRuntimeSettings(seed, []storage.RuntimeSetting{
		{Namespace: "python_toolchain", Key: "config", ValueJSON: `{"enabled":true,"python_executable":"C:/Python312/python.exe","uv_executable":"C:/Users/me/.local/bin/uv.exe","minimum_uv_version":"0.11.1"}`},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
	if !effective.PythonToolchain.Enabled {
		t.Fatalf("python_toolchain.enabled = false, want true")
	}
	if effective.PythonToolchain.PythonExecutable != "C:/Python312/python.exe" {
		t.Fatalf("python_executable = %q", effective.PythonToolchain.PythonExecutable)
	}
	if effective.PythonToolchain.UVExecutable != "C:/Users/me/.local/bin/uv.exe" {
		t.Fatalf("uv_executable = %q", effective.PythonToolchain.UVExecutable)
	}
	if effective.PythonToolchain.MinimumUVVersion != "0.11.1" {
		t.Fatalf("minimum_uv_version = %q", effective.PythonToolchain.MinimumUVVersion)
	}
	if effective.PythonToolchain.RequiredPython != "3.12" {
		t.Fatalf("required_python = %q, want default 3.12", effective.PythonToolchain.RequiredPython)
	}
}

func TestApplyRuntimeSettingsSupportsChatPromptRouter(t *testing.T) {
	seed := config.DefaultConfig()

	effective, issues := ApplyRuntimeSettings(seed, []storage.RuntimeSetting{
		{Namespace: "chat", Key: "config", ValueJSON: `{"prompt_router":{"mode":"always_work","sticky_turns":7}}`},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
	if effective.Chat.PromptRouter.Mode != config.PromptRouterModeAlwaysWork {
		t.Fatalf("chat.prompt_router.mode = %q, want always_work", effective.Chat.PromptRouter.Mode)
	}
	if effective.Chat.PromptRouter.StickyTurns != 7 {
		t.Fatalf("chat.prompt_router.sticky_turns = %d, want 7", effective.Chat.PromptRouter.StickyTurns)
	}
	if effective.Chat.PromptRouter.ContextTurns != 6 || effective.Chat.PromptRouter.MaxContextChars != 6000 {
		t.Fatalf("chat.prompt_router defaults = %#v", effective.Chat.PromptRouter)
	}
}

func TestApplyRuntimeSettingsRejectsInvalidChatPromptRouter(t *testing.T) {
	seed := config.DefaultConfig()

	_, issues := ApplyRuntimeSettings(seed, []storage.RuntimeSetting{
		{Namespace: "chat", Key: "config", ValueJSON: `{"prompt_router":{"mode":"bad"}}`},
	})
	if len(issues) != 1 {
		t.Fatalf("issues = %#v, want one issue", issues)
	}
	if issues[0].Path != "chat" || !strings.Contains(issues[0].Message, "prompt_router.mode") {
		t.Fatalf("issue = %#v, want chat prompt_router.mode issue", issues[0])
	}
}

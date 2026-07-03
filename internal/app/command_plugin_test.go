package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/chat"
	commandcore "github.com/longyisang/emoagent/internal/command"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestCommandServiceRegistersAndInvokesDirectPluginCommand(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	runtime := &fakeCommandPluginRuntime{result: plugin.CommandInvokeResult{
		Status:  "success",
		Content: "上海今天多云。",
		Payload: map[string]any{"city": "Shanghai"},
	}}
	commands.pluginRuntime = runtime
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister}, plugin.ManifestV2Command{
		Name:       "weather",
		RootName:   "weather",
		Summary:    "Weather lookup",
		Handler:    "weather.lookup",
		OutputMode: "direct",
	})
	conflicts := commands.RegisterPluginCommands(ctx, manifest)
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %#v, want none", conflicts)
	}
	origin := conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}
	sessionID := createTestSession(t, db, "session-plugin-direct", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/weather 上海 --unit c",
		Origin:     origin,
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/weather was not handled")
	}
	requireCommandMessage(t, resp, "weather", "success")
	if got := resp.Messages[len(resp.Messages)-1].Content; got != "上海今天多云。" {
		t.Fatalf("content = %q, want plugin result", got)
	}
	assertNoMessages(t, db, sessionID)
	assertInvocation(t, db, "plugin.com.example.weather.weather", "success")
	if runtime.calls != 1 {
		t.Fatalf("plugin calls = %d, want 1", runtime.calls)
	}
	if runtime.pluginID != "com.example.weather" || runtime.handler != "weather.lookup" {
		t.Fatalf("runtime target = %s/%s", runtime.pluginID, runtime.handler)
	}
	if runtime.lastInput.CommandID != "plugin.com.example.weather.weather" ||
		len(runtime.lastInput.Args) != 1 || runtime.lastInput.Args[0] != "上海" ||
		runtime.lastInput.Flags["unit"] != "c" ||
		runtime.lastInput.Context.OriginKey != origin.OriginKey ||
		runtime.lastInput.Context.SessionID != sessionID ||
		runtime.lastInput.Context.PersonaKey != "default" ||
		runtime.lastInput.Context.ActorRole != "member" {
		t.Fatalf("runtime input = %#v, want spec invoke_command shape", runtime.lastInput)
	}
}

func TestCommandServiceReportsPluginCommandConflict(t *testing.T) {
	ctx := context.Background()
	_, _, commands := newTestCommandService(t)
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister}, plugin.ManifestV2Command{
		Name:     "sid_alias",
		RootName: "sid",
		Handler:  "sid.lookup",
	})

	conflicts := commands.RegisterPluginCommands(ctx, manifest)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts len = %d, want 1", len(conflicts))
	}
	if conflicts[0].PluginID != "com.example.weather" || !strings.Contains(conflicts[0].Reason, "reserved") {
		t.Fatalf("conflict = %#v, want reserved plugin conflict", conflicts[0])
	}
	listed := commands.CommandConflicts()
	if len(listed) != 1 || listed[0].CommandID != conflicts[0].CommandID {
		t.Fatalf("listed conflicts = %#v, want %#v", listed, conflicts)
	}
}

func TestCommandServiceDisabledPluginCommandFailsBeforeRuntime(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	runtime := &fakeCommandPluginRuntime{result: plugin.CommandInvokeResult{Content: "should not run"}}
	commands.pluginRuntime = runtime
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister}, plugin.ManifestV2Command{
		Name:     "weather",
		RootName: "weather",
		Handler:  "weather.lookup",
	})
	commands.RegisterPluginCommands(ctx, manifest)
	if err := db.UpsertCommandConfig(ctx, storage.CommandConfigRecord{
		CommandID:     "plugin.com.example.weather.weather",
		ProviderKind:  "plugin",
		PluginID:      "com.example.weather",
		OriginalName:  "weather",
		EffectiveName: "weather",
		Enabled:       false,
		Permission:    "member",
		OutputMode:    "direct",
	}); err != nil {
		t.Fatalf("UpsertCommandConfig disabled: %v", err)
	}
	sessionID := createTestSession(t, db, "session-plugin-disabled", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/weather",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/weather was not handled")
	}
	requireCommandMessage(t, resp, "weather", "failed")
	if runtime.calls != 0 {
		t.Fatalf("runtime calls = %d, want 0", runtime.calls)
	}
}

func TestCommandServiceUpdatedRootAliasInvokesPluginCommand(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	runtime := &fakeCommandPluginRuntime{result: plugin.CommandInvokeResult{
		Status:  "success",
		Content: "forecast result",
	}}
	commands.pluginRuntime = runtime
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister}, plugin.ManifestV2Command{
		Name:     "weather",
		RootName: "weather",
		Handler:  "weather.lookup",
	})
	commands.RegisterPluginCommands(ctx, manifest)
	if err := commands.UpdateCommandConfig(ctx, storage.CommandConfigRecord{
		CommandID:     "plugin.com.example.weather.weather",
		EffectiveName: "forecast",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpdateCommandConfig: %v", err)
	}
	sessionID := createTestSession(t, db, "session-plugin-root-alias", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/forecast 上海",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/forecast was not handled")
	}
	requireCommandMessage(t, resp, "forecast", "success")
	if runtime.calls != 1 {
		t.Fatalf("plugin calls = %d, want 1", runtime.calls)
	}
}

func TestCommandServiceAppliesStoredPluginRootAliasOnRegistration(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	runtime := &fakeCommandPluginRuntime{result: plugin.CommandInvokeResult{Status: "success", Content: "forecast result"}}
	commands.pluginRuntime = runtime
	if err := db.UpsertCommandConfig(ctx, storage.CommandConfigRecord{
		CommandID:     "plugin.com.example.weather.weather",
		ProviderKind:  "plugin",
		PluginID:      "com.example.weather",
		OriginalName:  "weather",
		EffectiveName: "forecast",
		Enabled:       true,
		Permission:    "member",
		OutputMode:    "direct",
	}); err != nil {
		t.Fatalf("UpsertCommandConfig: %v", err)
	}
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister}, plugin.ManifestV2Command{
		Name:     "weather",
		RootName: "weather",
		Handler:  "weather.lookup",
	})
	commands.RegisterPluginCommands(ctx, manifest)
	sessionID := createTestSession(t, db, "session-plugin-stored-root-alias", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/forecast 上海",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/forecast was not handled")
	}
	requireCommandMessage(t, resp, "forecast", "success")
}

func TestCommandServiceLLMSynthesizeWithoutProviderCapabilityIsDenied(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	commands.pluginRuntime = &fakeCommandPluginRuntime{result: plugin.CommandInvokeResult{Content: "raw"}}
	err := commands.Registry().TryRegister(commandcore.CommandDescriptor{
		ID:           "plugin.com.example.weather.weather",
		Name:         "weather",
		ProviderKind: commandcore.CommandProviderPlugin,
		PluginID:     "com.example.weather",
		OutputMode:   commandcore.CommandOutputLLMSynthesize,
		Permission:   commandcore.CommandPermissionMember,
	})
	if err != nil {
		t.Fatalf("TryRegister: %v", err)
	}
	sessionID := createTestSession(t, db, "session-plugin-gated", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/weather",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/weather was not handled")
	}
	requireCommandMessage(t, resp, "weather", "failed")
	assertInvocation(t, db, "plugin.com.example.weather.weather", "failed")
}

func TestCommandServicePluginCommandTimeoutAddsDeadline(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	runtime := &fakeCommandPluginRuntime{result: plugin.CommandInvokeResult{
		Status:  "success",
		Content: "ok",
	}}
	commands.pluginRuntime = runtime
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister}, plugin.ManifestV2Command{
		Name:      "weather",
		RootName:  "weather",
		Handler:   "weather.lookup",
		TimeoutMS: 500,
	})
	commands.RegisterPluginCommands(ctx, manifest)
	sessionID := createTestSession(t, db, "session-plugin-timeout", "default")

	_, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/weather",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/weather was not handled")
	}
	if runtime.deadline.IsZero() {
		t.Fatal("runtime deadline is zero, want timeout deadline")
	}
	if time.Until(runtime.deadline) <= 0 || time.Until(runtime.deadline) > time.Second {
		t.Fatalf("runtime deadline = %s, want near future", runtime.deadline)
	}
}

func TestCommandServiceLLMSynthesizeCallsLLMOnce(t *testing.T) {
	ctx := context.Background()
	app, db, commands := newTestCommandService(t)
	runtime := &fakeCommandPluginRuntime{result: plugin.CommandInvokeResult{
		Status:  "success",
		Content: "raw plugin result",
	}}
	commands.pluginRuntime = runtime
	synth := &commandPluginSynthesisFakeLLM{content: "synthesized command result"}
	setTestActiveRuntime(app, &ActiveAgentRuntime{
		PersonaKey: "default",
		EmotionSummary: ModelRuntime{
			Client: synth,
			Model:  "summary-model",
		},
	})
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister, plugin.CapabilityProviderGenerate}, plugin.ManifestV2Command{
		Name:       "weather",
		RootName:   "weather",
		Summary:    "Weather lookup",
		Handler:    "weather.lookup",
		OutputMode: "llm_synthesize",
	})
	commands.RegisterPluginCommands(ctx, manifest)
	sessionID := createTestSession(t, db, "session-plugin-synth", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/weather 上海",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/weather was not handled")
	}
	requireCommandMessage(t, resp, "weather", "success")
	if runtime.calls != 1 {
		t.Fatalf("plugin calls = %d, want 1", runtime.calls)
	}
	if synth.calls != 1 {
		t.Fatalf("synthesis calls = %d, want 1", synth.calls)
	}
	if len(synth.lastRequest.Tools) != 0 {
		t.Fatalf("synthesis tools = %#v, want none", synth.lastRequest.Tools)
	}
	if got := resp.Messages[len(resp.Messages)-1].Content; got != "synthesized command result" {
		t.Fatalf("content = %q, want synthesized result", got)
	}
	assertNoMessages(t, db, sessionID)
	assertInvocation(t, db, "plugin.com.example.weather.weather", "success")
}

func TestCommandServiceLLMSynthesizeDeniedByProviderGrantSkipsRuntimeAndLLM(t *testing.T) {
	ctx := context.Background()
	app, db, commands := newTestCommandService(t)
	runtime := &fakeCommandPluginRuntime{
		authorizeErr: plugin.ErrCapabilityDenied,
		result:       plugin.CommandInvokeResult{Status: "success", Content: "raw plugin result"},
	}
	commands.pluginRuntime = runtime
	synth := &commandPluginSynthesisFakeLLM{content: "should not run"}
	setTestActiveRuntime(app, &ActiveAgentRuntime{
		PersonaKey: "default",
		EmotionSummary: ModelRuntime{
			Client: synth,
			Model:  "summary-model",
		},
	})
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister, plugin.CapabilityProviderGenerate}, plugin.ManifestV2Command{
		Name:       "weather",
		RootName:   "weather",
		Handler:    "weather.lookup",
		OutputMode: "llm_synthesize",
	})
	commands.RegisterPluginCommands(ctx, manifest)
	sessionID := createTestSession(t, db, "session-plugin-synth-denied", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/weather 上海",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/weather was not handled")
	}
	requireCommandMessage(t, resp, "weather", "failed")
	if runtime.authorizeCalls != 1 {
		t.Fatalf("authorize calls = %d, want 1", runtime.authorizeCalls)
	}
	if runtime.calls != 0 {
		t.Fatalf("plugin calls = %d, want 0", runtime.calls)
	}
	if synth.calls != 0 {
		t.Fatalf("synthesis calls = %d, want 0", synth.calls)
	}
	assertInvocation(t, db, "plugin.com.example.weather.weather", "failed")
}

func TestCommandServiceLLMSynthesizeDeniedByPluginServiceGrantSkipsRuntimeAndLLM(t *testing.T) {
	ctx := context.Background()
	app, db, commands := newTestCommandService(t)
	synth := &commandPluginSynthesisFakeLLM{content: "should not run"}
	setTestActiveRuntime(app, &ActiveAgentRuntime{
		PersonaKey: "default",
		EmotionSummary: ModelRuntime{
			Client: synth,
			Model:  "summary-model",
		},
	})
	broker := plugin.NewFacadeBroker(db, nil)
	pluginService := &PluginService{facadeBroker: broker}
	commands.pluginRuntime = pluginService
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister, plugin.CapabilityProviderGenerate}, plugin.ManifestV2Command{
		Name:       "weather",
		RootName:   "weather",
		Handler:    "weather.lookup",
		OutputMode: "llm_synthesize",
	})
	broker.AddPlugin(manifest)
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, `{"tier":"runtime_safe","capabilities":["command.register"]}`); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	commands.RegisterPluginCommands(ctx, manifest)
	sessionID := createTestSession(t, db, "session-plugin-synth-grant-denied", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/weather 上海",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/weather was not handled")
	}
	requireCommandMessage(t, resp, "weather", "failed")
	if synth.calls != 0 {
		t.Fatalf("synthesis calls = %d, want 0", synth.calls)
	}
	assertInvocation(t, db, "plugin.com.example.weather.weather", "failed")
}

func TestCommandServiceLLMSynthesizeSkipsLLMWhenPluginFailed(t *testing.T) {
	ctx := context.Background()
	app, db, commands := newTestCommandService(t)
	runtime := &fakeCommandPluginRuntime{result: plugin.CommandInvokeResult{
		Status:  "failed",
		Content: "plugin failed",
	}}
	commands.pluginRuntime = runtime
	synth := &commandPluginSynthesisFakeLLM{content: "should not run"}
	setTestActiveRuntime(app, &ActiveAgentRuntime{
		PersonaKey: "default",
		EmotionSummary: ModelRuntime{
			Client: synth,
			Model:  "summary-model",
		},
	})
	manifest := testCommandManifest([]plugin.Capability{plugin.CapabilityCommandRegister, plugin.CapabilityProviderGenerate}, plugin.ManifestV2Command{
		Name:       "weather",
		RootName:   "weather",
		Handler:    "weather.lookup",
		OutputMode: "llm_synthesize",
	})
	commands.RegisterPluginCommands(ctx, manifest)
	sessionID := createTestSession(t, db, "session-plugin-synth-failed", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/weather 上海",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/weather was not handled")
	}
	requireCommandMessage(t, resp, "weather", "failed")
	if synth.calls != 0 {
		t.Fatalf("synthesis calls = %d, want 0", synth.calls)
	}
	if got := resp.Messages[len(resp.Messages)-1].Content; got != "plugin failed" {
		t.Fatalf("content = %q, want raw failed result", got)
	}
}

type fakeCommandPluginRuntime struct {
	calls          int
	authorizeCalls int
	authorizeErr   error
	pluginID       string
	handler        string
	lastInput      plugin.CommandInvokeRequest
	deadline       time.Time
	result         plugin.CommandInvokeResult
	invokeErr      error
}

func (f *fakeCommandPluginRuntime) AuthorizeProviderGenerate(context.Context, string) error {
	f.authorizeCalls++
	return f.authorizeErr
}

func (f *fakeCommandPluginRuntime) InvokeCommand(ctx context.Context, pluginID string, input plugin.CommandInvokeRequest) (plugin.CommandInvokeResult, error) {
	f.calls++
	f.pluginID = pluginID
	f.handler = input.Handler
	f.lastInput = input
	f.deadline, _ = ctx.Deadline()
	return f.result, f.invokeErr
}

type commandPluginSynthesisFakeLLM struct {
	calls       int
	content     string
	lastRequest llm.ChatRequest
}

func (f *commandPluginSynthesisFakeLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.calls++
	f.lastRequest = req
	return &llm.ChatResponse{Content: f.content}, nil
}

func (f *commandPluginSynthesisFakeLLM) ChatStream(ctx context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	return f.Chat(ctx, req)
}

func testCommandManifest(capabilities []plugin.Capability, commands ...plugin.ManifestV2Command) plugin.ManifestV2 {
	return plugin.ManifestV2{
		SchemaVersion:   plugin.ManifestSchemaV02,
		ID:              "com.example.weather",
		Name:            "Weather Plugin",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         plugin.ManifestV2Runtime{Kind: plugin.RuntimeManagedPythonProcess, Entry: "main.py"},
		Access:          plugin.ManifestV2Access{Tier: plugin.AccessTierRuntimeSafe, Capabilities: capabilities},
		Commands:        commands,
	}
}

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/chat"
	commandcore "github.com/longyisang/emoagent/internal/command"
	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
)

type CommandService struct {
	core          *commandcore.CommandService
	infra         *Infra
	conversation  *ConversationService
	memory        *MemoryService
	agentRuntime  *AgentRuntimeService
	chat          *ChatService
	pluginRuntime pluginCommandRuntime
	conflicts     []CommandConflict
}

type pluginCommandRuntime interface {
	AuthorizeProviderGenerate(context.Context, string) error
	InvokeCommand(context.Context, string, plugin.CommandInvokeRequest) (plugin.CommandInvokeResult, error)
}

type CommandConflict struct {
	CommandID string
	PluginID  string
	RootName  string
	Reason    string
}

func NewCommandService() *CommandService {
	registry := commandcore.NewRegistry()
	for _, descriptor := range commandcore.NewBuiltinProvider().Descriptors() {
		_ = registry.TryRegister(descriptor)
	}
	return &CommandService{core: commandcore.NewCommandService(registry)}
}

func (s *CommandService) configure(infra *Infra, conversation *ConversationService, memory *MemoryService, agentRuntime *AgentRuntimeService) {
	if s == nil {
		return
	}
	s.infra = infra
	s.conversation = conversation
	s.memory = memory
	s.agentRuntime = agentRuntime
}

func (s *CommandService) Core() *commandcore.CommandService {
	if s == nil {
		return nil
	}
	return s.core
}

func (s *CommandService) Registry() *commandcore.Registry {
	if s == nil || s.core == nil {
		return nil
	}
	return s.core.Registry()
}

func (s *CommandService) TryHandle(ctx context.Context, req chat.CommandRequest) (chat.CommandResponse, bool, error) {
	if s == nil || s.core == nil {
		return chat.CommandResponse{}, false, nil
	}
	parsed, descriptor, handled, parseErr := s.core.TryParse(req.Content)
	if !handled {
		return chat.CommandResponse{}, false, nil
	}
	if req.PersonaKey == "" {
		req.PersonaKey = "default"
	}
	if req.ActorRole == "" {
		req.ActorRole = string(commandcore.CommandPermissionMember)
	}
	if descriptor.ID == "" {
		descriptor = unknownDescriptor(parsed.Name)
	} else if config, err := s.commandConfig(ctx, descriptor.ID); err != nil {
		exec := commandExecution{service: s, request: req, parsed: parsed, descriptor: descriptor, started: time.Now()}
		return exec.finish(ctx, commandResult{Status: "failed", Content: "读取命令配置失败：" + err.Error(), ErrorKind: "internal_error"}), true, nil
	} else {
		if config != nil && !config.Enabled {
			exec := commandExecution{service: s, request: req, parsed: parsed, descriptor: descriptor, started: time.Now()}
			return exec.finish(ctx, commandResult{Status: "failed", Content: "命令已禁用。", ErrorKind: "disabled"}), true, nil
		}
		descriptor = applyCommandConfig(descriptor, config)
		if err := s.core.CheckPermission(commandcore.CommandActor{Role: commandcore.CommandPermission(req.ActorRole)}, descriptor); err != nil {
			exec := commandExecution{service: s, request: req, parsed: parsed, descriptor: descriptor, started: time.Now()}
			return exec.finish(ctx, commandResult{Status: "failed", Content: "权限不足：" + err.Error(), ErrorKind: "permission_denied"}), true, nil
		}
	}

	exec := commandExecution{
		service:    s,
		request:    req,
		parsed:     parsed,
		descriptor: descriptor,
		started:    time.Now(),
	}
	if parseErr != nil {
		return exec.finish(ctx, commandResult{
			Status:    "failed",
			Content:   "命令解析失败：" + parseErr.Error(),
			ErrorKind: "validation_error",
		}), true, nil
	}

	switch canonicalCommandName(descriptor) {
	case "help":
		return exec.finish(ctx, s.handleHelp()), true, nil
	case "sid":
		return exec.finish(ctx, s.handleSID(req)), true, nil
	case "new":
		return exec.finish(ctx, s.handleNew(ctx, req)), true, nil
	case "switch":
		return exec.finish(ctx, s.handleSwitch(ctx, req, parsed)), true, nil
	case "reset":
		return exec.finish(ctx, s.handleReset(ctx, req)), true, nil
	case "clear":
		return exec.finish(ctx, s.handleClear(ctx, req)), true, nil
	case "compact":
		return exec.finish(ctx, s.handleCompact(ctx, req)), true, nil
	case "forget":
		return exec.finish(ctx, s.handleForget(ctx, req, parsed)), true, nil
	case "stop":
		return exec.finish(ctx, s.handleStop(req)), true, nil
	default:
		if descriptor.ProviderKind == commandcore.CommandProviderPlugin {
			return exec.finish(ctx, s.handlePluginCommand(ctx, req, parsed, descriptor)), true, nil
		}
		return exec.finish(ctx, commandResult{
			Status:    "failed",
			Content:   fmt.Sprintf("未知命令：/%s", parsed.Name),
			ErrorKind: "validation_error",
		}), true, nil
	}
}

func (s *CommandService) RegisterPluginCommands(ctx context.Context, manifest plugin.ManifestV2) []CommandConflict {
	if s == nil || s.Registry() == nil {
		return nil
	}
	conflicts := make([]CommandConflict, 0)
	for _, command := range manifest.Commands {
		descriptor := pluginCommandDescriptor(manifest, command)
		if descriptor.OutputMode == "" {
			descriptor.OutputMode = commandcore.CommandOutputDirect
		}
		if err := s.Registry().TryRegister(descriptor); err != nil {
			conflict := CommandConflict{
				CommandID: descriptor.ID,
				PluginID:  manifest.ID,
				RootName:  descriptor.Name,
				Reason:    err.Error(),
			}
			conflicts = append(conflicts, conflict)
			continue
		}
		s.ensureCommandConfig(ctx, descriptor)
		if err := s.applyStoredCommandConfig(ctx, descriptor.ID); err != nil {
			conflicts = append(conflicts, CommandConflict{
				CommandID: descriptor.ID,
				PluginID:  manifest.ID,
				RootName:  descriptor.Name,
				Reason:    err.Error(),
			})
		}
	}
	s.conflicts = append(s.conflicts, conflicts...)
	return conflicts
}

func (s *CommandService) UnregisterPluginCommands(pluginID string) int {
	if s == nil || s.Registry() == nil {
		return 0
	}
	pluginID = strings.TrimSpace(pluginID)
	removed := s.Registry().UnregisterPlugin(pluginID)
	if removed == 0 || len(s.conflicts) == 0 {
		return removed
	}
	conflicts := s.conflicts[:0]
	for _, conflict := range s.conflicts {
		if strings.TrimSpace(conflict.PluginID) != pluginID {
			conflicts = append(conflicts, conflict)
		}
	}
	s.conflicts = conflicts
	return removed
}

func (s *CommandService) LoadCommandConfigs(ctx context.Context) {
	if s == nil || s.infra == nil || s.infra.DB == nil || s.Registry() == nil {
		return
	}
	configs, err := s.infra.DB.ListCommandConfigs(ctx)
	if err != nil {
		return
	}
	for _, config := range configs {
		if _, ok := s.Registry().Get(config.CommandID); !ok {
			continue
		}
		if err := s.applyCommandConfigRoots(config); err != nil {
			s.conflicts = append(s.conflicts, CommandConflict{
				CommandID: config.CommandID,
				PluginID:  config.PluginID,
				RootName:  config.EffectiveName,
				Reason:    err.Error(),
			})
		}
	}
}

func (s *CommandService) CommandConflicts() []CommandConflict {
	if s == nil {
		return nil
	}
	return append([]CommandConflict(nil), s.conflicts...)
}

func (s *CommandService) ListCommandDescriptors() []commandcore.CommandDescriptor {
	if s == nil || s.Registry() == nil {
		return nil
	}
	return s.Registry().Descriptors()
}

func (s *CommandService) ListCommandConfigs(ctx context.Context) ([]storage.CommandConfigRecord, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	return s.infra.DB.ListCommandConfigs(ctx)
}

func (s *CommandService) UpdateCommandConfig(ctx context.Context, config storage.CommandConfigRecord) error {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	var aliases []string
	if strings.TrimSpace(config.AliasesJSON) != "" {
		if err := json.Unmarshal([]byte(config.AliasesJSON), &aliases); err != nil {
			return fmt.Errorf("decode command aliases: %w", err)
		}
	}
	if existing, err := s.infra.DB.GetCommandConfig(ctx, config.CommandID); err != nil {
		return err
	} else if existing != nil {
		if config.ProviderKind == "" {
			config.ProviderKind = existing.ProviderKind
		}
		if config.PluginID == "" {
			config.PluginID = existing.PluginID
		}
		if config.OriginalName == "" {
			config.OriginalName = existing.OriginalName
		}
		if config.EffectiveName == "" {
			config.EffectiveName = existing.EffectiveName
		}
		if config.AliasesJSON == "" {
			config.AliasesJSON = existing.AliasesJSON
			_ = json.Unmarshal([]byte(existing.AliasesJSON), &aliases)
		}
		if config.Permission == "" {
			config.Permission = existing.Permission
		}
		if config.OutputMode == "" {
			config.OutputMode = existing.OutputMode
		}
		if config.ConfigJSON == "" {
			config.ConfigJSON = existing.ConfigJSON
		}
	}
	if strings.TrimSpace(config.EffectiveName) != "" && s.Registry() != nil {
		if err := s.Registry().UpdateRoots(config.CommandID, config.EffectiveName, aliases); err != nil {
			return err
		}
	}
	return s.infra.DB.UpsertCommandConfig(ctx, config)
}

func (s *CommandService) ListCommandInvocations(ctx context.Context, filter storage.CommandInvocationFilter) ([]storage.CommandInvocationRecord, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	return s.infra.DB.ListCommandInvocations(ctx, filter)
}

func (s *CommandService) handleHelp() commandResult {
	descriptors := s.Registry().Descriptors()
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.Hidden {
			continue
		}
		names = append(names, "/"+descriptor.Name)
	}
	return commandResult{
		Status:  "success",
		Content: "可用命令：" + strings.Join(names, "、"),
	}
}

func (s *CommandService) handleSID(req chat.CommandRequest) commandResult {
	return commandResult{
		Status: "success",
		Content: fmt.Sprintf("origin_key=%s\nsession_id=%s\npersona=%s",
			req.Origin.OriginKey, req.SessionID, req.PersonaKey),
		Payload: map[string]any{
			"origin_key": req.Origin.OriginKey,
			"session_id": req.SessionID,
			"persona":    req.PersonaKey,
		},
	}
}

func (s *CommandService) handleNew(ctx context.Context, req chat.CommandRequest) commandResult {
	warnings := s.stopRuns(req)
	warnings = append(warnings, s.rolloverMemory(ctx, req.SessionID, req.PersonaKey, "command_new")...)
	sessionID, err := s.startSession(ctx, req.PersonaKey)
	if err != nil {
		return commandResult{Status: "failed", Content: "创建新会话失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	if err := s.bindSession(ctx, req.Origin, req.PersonaKey, sessionID, true); err != nil {
		return commandResult{Status: "failed", Content: "绑定新会话失败：" + err.Error(), ErrorKind: "internal_error", SessionID: sessionID}
	}
	return commandResult{
		Status:        statusWithWarnings(warnings),
		Content:       "已切换到新会话。",
		SessionID:     sessionID,
		ReloadHistory: true,
		ReloadMemory:  true,
		ContextSwitch: "new",
		Payload:       warningsPayload(warnings),
	}
}

func (s *CommandService) handleSwitch(ctx context.Context, req chat.CommandRequest, parsed commandcore.ParsedCommand) commandResult {
	if len(parsed.Args) == 0 || strings.TrimSpace(parsed.Args[0]) == "" {
		return commandResult{Status: "failed", Content: "用法：/switch <session_id>", ErrorKind: "validation_error"}
	}
	sessionID := strings.TrimSpace(parsed.Args[0])
	session, err := s.getSession(ctx, sessionID)
	if err != nil {
		return commandResult{Status: "failed", Content: "读取会话失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	if session == nil {
		return commandResult{Status: "failed", Content: "会话不存在：" + sessionID, ErrorKind: "validation_error"}
	}
	if session.Persona != "" && req.PersonaKey != "" && session.Persona != req.PersonaKey {
		return commandResult{Status: "failed", Content: "目标会话不属于当前 persona。", ErrorKind: "validation_error"}
	}
	if engine := s.engine(); engine != nil {
		if resumedID, resumed, err := engine.ResumeSession(ctx, sessionID, req.PersonaKey); err != nil {
			return commandResult{Status: "failed", Content: "恢复会话失败：" + err.Error(), ErrorKind: "internal_error"}
		} else if resumed {
			sessionID = resumedID
		}
	}
	if err := s.bindSession(ctx, req.Origin, req.PersonaKey, sessionID, false); err != nil {
		return commandResult{Status: "failed", Content: "绑定会话失败：" + err.Error(), ErrorKind: "internal_error", SessionID: sessionID}
	}
	return commandResult{
		Status:        "success",
		Content:       "已切换到指定会话。",
		SessionID:     sessionID,
		ReloadHistory: true,
		ReloadMemory:  true,
		ContextSwitch: "switch",
	}
}

func (s *CommandService) handleReset(ctx context.Context, req chat.CommandRequest) commandResult {
	warnings := s.stopRuns(req)
	latestID, err := s.latestMessageID(ctx, req.SessionID)
	if err != nil {
		return commandResult{Status: "failed", Content: "读取最新消息失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	cfg := config.DefaultConfig().Context
	if s.agentRuntime != nil {
		cfg = s.agentRuntime.GlobalContextConfig()
	}
	previous, err := contextutil.LoadSessionState(ctx, s.infra.DB, req.SessionID, cfg)
	if err != nil {
		return commandResult{Status: "failed", Content: "读取上下文状态失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	epoch := 1
	if previous != nil && previous.ResetBarrier != nil {
		epoch = previous.ResetBarrier.Epoch + 1
	}
	next := contextutil.ContextState{
		ContextVersion:      contextutil.CurrentContextVersion,
		Mode:                contextutil.ModeEmotion,
		KeepRecentUserTurns: cfg.KeepRecentUserTurns,
		ResetBarrier: &contextutil.ContextResetBarrier{
			Epoch:          epoch,
			AfterMessageID: latestID,
			ResetAt:        time.Now().UTC().Format(time.RFC3339),
			Reason:         "command_reset",
		},
	}
	if err := contextutil.UpdateSessionContextState(ctx, s.infra.DB, req.SessionID, next); err != nil {
		return commandResult{Status: "failed", Content: "写入上下文状态失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	warnings = append(warnings, s.rolloverMemory(ctx, req.SessionID, req.PersonaKey, "command_reset")...)
	return commandResult{
		Status:        statusWithWarnings(warnings),
		Content:       "已重置当前会话的 LLM 上下文。聊天记录和长期记忆没有删除。",
		ReloadHistory: false,
		ReloadMemory:  true,
		ContextSwitch: "reset",
		Payload:       warningsPayload(warnings),
	}
}

func (s *CommandService) handleClear(ctx context.Context, req chat.CommandRequest) commandResult {
	if err := s.ensureOrigin(ctx, req.Origin); err != nil {
		return commandResult{Status: "failed", Content: "写入 origin 失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	latestID, err := s.latestMessageID(ctx, req.SessionID)
	if err != nil {
		return commandResult{Status: "failed", Content: "读取最新消息失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	if err := s.infra.DB.UpsertSessionClearMarker(ctx, storage.SessionClearMarkerRecord{
		OriginKey:      req.Origin.OriginKey,
		SessionID:      req.SessionID,
		PersonaKey:     req.PersonaKey,
		AfterMessageID: latestID,
		Reason:         "command_clear",
		MetadataJSON:   "{}",
	}); err != nil {
		return commandResult{Status: "failed", Content: "写入可见历史清理标记失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	return commandResult{
		Status:        "success",
		Content:       "已清理当前窗口可见历史。上下文和记忆没有改变。",
		ReloadHistory: true,
	}
}

func (s *CommandService) handleCompact(ctx context.Context, req chat.CommandRequest) commandResult {
	active := (*ActiveAgentRuntime)(nil)
	if s.agentRuntime != nil {
		active = s.agentRuntime.Active()
	}
	if active == nil || active.EmotionSummary.Client == nil {
		return commandResult{Status: "success", Content: "当前没有可用的总结模型，未压缩上下文。"}
	}
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return commandResult{Status: "failed", Content: "数据库未配置。", ErrorKind: "internal_error"}
	}
	cfg := active.Context
	if err := cfg.Validate(); err != nil {
		if s.agentRuntime != nil {
			cfg = s.agentRuntime.GlobalContextConfig()
		} else {
			cfg = config.DefaultConfig().Context
		}
	}
	state, err := contextutil.LoadSessionState(ctx, s.infra.DB, req.SessionID, cfg)
	if err != nil {
		return commandResult{Status: "failed", Content: "读取上下文状态失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	history, err := s.infra.DB.GetAllMessages(ctx, req.SessionID)
	if err != nil {
		return commandResult{Status: "failed", Content: "读取消息失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	history = contextutil.FilterHistoryAfterResetBarrier(history, state.ResetBarrier)
	persona := &config.Persona{Name: req.PersonaKey}
	next, report, err := contextutil.UpdateRunningSummaryWithParams(ctx, active.EmotionSummary.Client, active.EmotionSummary.Model, active.EmotionSummary.Params, persona, history, state, cfg)
	if err != nil {
		return commandResult{
			Status:    "failed",
			Content:   "压缩上下文失败：" + err.Error(),
			ErrorKind: "internal_error",
			Payload:   summaryReportPayload(report),
		}
	}
	if err := contextutil.UpdateSessionContextState(ctx, s.infra.DB, req.SessionID, *next); err != nil {
		return commandResult{Status: "failed", Content: "写入上下文状态失败：" + err.Error(), ErrorKind: "internal_error"}
	}
	content := "当前没有需要压缩的新上下文。"
	if report.Attempted {
		content = "已压缩当前会话上下文。"
	}
	return commandResult{
		Status:  "success",
		Content: content,
		Payload: summaryReportPayload(report),
	}
}

func (s *CommandService) handleForget(ctx context.Context, req chat.CommandRequest, parsed commandcore.ParsedCommand) commandResult {
	target := strings.TrimSpace(strings.Join(parsed.Args, " "))
	if target == "" {
		return commandResult{Status: "failed", Content: "用法：/forget <target>", ErrorKind: "validation_error"}
	}
	if s == nil || s.memory == nil || s.memory.Host() == nil || s.memory.Host().Core == nil {
		return commandResult{Status: "success", Content: "已识别遗忘请求，但当前实现需要后续 Forget Manager 接入。", Payload: map[string]any{"target": target}}
	}
	personaID := strings.TrimSpace(req.PersonaKey)
	if personaID == "" {
		personaID = "default"
	}
	previewReq := memorycore.ForgetPreviewRequest{
		RequestID:           "manual_forget_" + uuid.NewString(),
		PersonaID:           personaID,
		Actor:               memorycore.ForgetActorUser,
		RequestedLevel:      memorycore.ForgetLevelSoft,
		ScopeMode:           memorycore.ForgetScopeSemanticQuery,
		ChatSessionID:       strings.TrimSpace(req.SessionID),
		Limit:               5,
		SemanticQuery:       &target,
		RequireConfirmation: true,
	}
	if s.infra != nil && s.infra.DB != nil && strings.TrimSpace(req.SessionID) != "" {
		segments, err := s.infra.DB.ListMemorySegments(ctx, storage.ListMemorySegmentsFilter{ChatSessionID: req.SessionID, Limit: 100})
		if err != nil {
			return commandResult{Status: "failed", Content: "读取记忆分段失败：" + err.Error(), ErrorKind: "internal_error"}
		}
		if len(segments) > 0 {
			segment := segments[len(segments)-1]
			previewReq.SessionID = strings.TrimSpace(segment.MemorySessionID)
			previewReq.RequestEpisodeID = strings.TrimSpace(segment.LastUserEpisodeID)
		}
	}
	preview, err := s.memory.Host().Core.PreviewForget(ctx, previewReq)
	if err != nil {
		return commandResult{Status: "success", Content: "我暂时无法生成可删除候选，未执行删除。", Payload: map[string]any{"target": target}}
	}
	if preview == nil || len(preview.Targets) == 0 {
		return commandResult{Status: "success", Content: "我没有找到可安全删除的候选，未执行删除。", Payload: map[string]any{"target": target}}
	}
	if strings.TrimSpace(preview.PreviewHash) == "" {
		return commandResult{Status: "success", Content: "删除预览缺少校验信息，未执行删除。", Payload: map[string]any{"target": target}}
	}
	if strings.TrimSpace(preview.OperationID) == "" {
		return commandResult{Status: "success", Content: "删除预览缺少确认操作信息，未执行删除。", Payload: map[string]any{"target": target}}
	}
	if strings.TrimSpace(preview.RequestID) == "" {
		preview.RequestID = previewReq.RequestID
	}
	if strings.TrimSpace(preview.RequestedLevel) == "" {
		preview.RequestedLevel = memorycore.ForgetLevelSoft
	}
	if strings.TrimSpace(preview.ScopeMode) == "" {
		preview.ScopeMode = memorycore.ForgetScopeSemanticQuery
	}
	return commandResult{
		Status:  "success",
		Content: memoryhost.BuildManualForgetPreviewNotice(*preview),
		Payload: map[string]any{
			"target":       target,
			"operation_id": preview.OperationID,
			"preview_hash": preview.PreviewHash,
		},
	}
}

func (s *CommandService) handleStop(req chat.CommandRequest) commandResult {
	count := 0
	if s.conversation != nil && s.conversation.RunRegistry() != nil {
		count = s.conversation.RunRegistry().Stop(conversation.StopSelector{
			OriginKey: req.Origin.OriginKey,
			SessionID: req.SessionID,
		})
	}
	if count == 0 {
		return commandResult{
			Status:  "success",
			Content: "没有正在运行的回复。",
			Payload: map[string]any{"stopped_count": 0},
		}
	}
	return commandResult{
		Status:  "success",
		Content: fmt.Sprintf("已停止 %d 个正在运行的回复。", count),
		Payload: map[string]any{"stopped_count": count},
	}
}

func (s *CommandService) handlePluginCommand(ctx context.Context, req chat.CommandRequest, parsed commandcore.ParsedCommand, descriptor commandcore.CommandDescriptor) commandResult {
	if descriptor.OutputMode == commandcore.CommandOutputLLMSynthesize && !descriptorHasCapability(descriptor, string(plugin.CapabilityProviderGenerate)) {
		return commandResult{Status: "failed", Content: "插件命令缺少 provider.generate capability，不能启用 LLM synthesis。", ErrorKind: plugin.ErrorKindPluginCapabilityDenied}
	}
	if s.pluginRuntime == nil {
		return commandResult{Status: "failed", Content: "插件命令运行时未配置。", ErrorKind: "plugin_runtime_unavailable"}
	}
	if descriptor.OutputMode == commandcore.CommandOutputLLMSynthesize {
		if err := s.pluginRuntime.AuthorizeProviderGenerate(ctx, descriptor.PluginID); err != nil {
			return commandResult{Status: "failed", Content: "插件命令没有授权 provider.generate，不能启用 LLM synthesis。", ErrorKind: plugin.ErrorKindPluginCapabilityDenied}
		}
	}
	input := plugin.CommandInvokeRequest{
		CommandID: descriptor.ID,
		Handler:   descriptor.Handler,
		Args:      append([]string(nil), parsed.Args...),
		Flags:     cloneStringMap(parsed.Flags),
		Context: plugin.CommandInvokeContext{
			OriginKey:  req.Origin.OriginKey,
			SessionID:  req.SessionID,
			PersonaKey: req.PersonaKey,
			ActorRole:  req.ActorRole,
		},
	}
	invokeCtx := ctx
	cancel := func() {}
	if descriptor.TimeoutMS > 0 {
		invokeCtx, cancel = context.WithTimeout(ctx, time.Duration(descriptor.TimeoutMS)*time.Millisecond)
	}
	defer cancel()
	result, err := s.pluginRuntime.InvokeCommand(invokeCtx, descriptor.PluginID, input)
	if err != nil {
		return commandResult{Status: "failed", Content: "插件命令执行失败：" + err.Error(), ErrorKind: plugin.ErrorKindPluginHookFailed}
	}
	status := strings.TrimSpace(result.Status)
	if status == "" {
		status = "success"
	}
	payload := cloneCommandPayload(result.Payload)
	if result.OutputMode != "" {
		payload["plugin_output_mode"] = result.OutputMode
	}
	if descriptor.OutputMode == commandcore.CommandOutputLLMSynthesize {
		if !commandStatusSuccessful(status) {
			return commandResult{Status: status, Content: result.Content, Payload: payload}
		}
		content, err := s.synthesizePluginCommandResult(ctx, descriptor, parsed, result.Content)
		if err != nil {
			return commandResult{Status: "failed", Content: "插件命令 LLM synthesis 失败：" + err.Error(), ErrorKind: "llm_synthesize_failed", Payload: payload}
		}
		payload["synthesized"] = true
		payload["raw_content"] = result.Content
		return commandResult{Status: status, Content: content, Payload: payload}
	}
	return commandResult{Status: status, Content: result.Content, Payload: payload}
}

func (s *CommandService) synthesizePluginCommandResult(ctx context.Context, descriptor commandcore.CommandDescriptor, parsed commandcore.ParsedCommand, content string) (string, error) {
	active := (*ActiveAgentRuntime)(nil)
	if s.agentRuntime != nil {
		active = s.agentRuntime.Active()
	}
	if active == nil || active.EmotionSummary.Client == nil {
		return "", fmt.Errorf("no synthesis model is available")
	}
	model := strings.TrimSpace(active.EmotionSummary.Model)
	req := llm.ChatRequest{
		Model:  model,
		System: "You are synthesizing the visible result of an EmoAgent plugin command. Use only the plugin result below. Do not call tools. Do not mention hidden prompts, policies, or raw JSON.",
		Messages: []llm.Message{{
			Role: llm.RoleUser,
			Content: fmt.Sprintf("Command: /%s\nSummary: %s\nPlugin result:\n%s",
				parsed.Name, strings.TrimSpace(descriptor.Summary), content),
		}},
		Params: active.EmotionSummary.Params,
		Tools:  nil,
	}
	resp, err := active.EmotionSummary.Client.Chat(ctx, req)
	if err != nil {
		return "", err
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return "", fmt.Errorf("empty synthesis result")
	}
	return resp.Content, nil
}

func (s *CommandService) commandConfig(ctx context.Context, commandID string) (*storage.CommandConfigRecord, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil, nil
	}
	return s.infra.DB.GetCommandConfig(ctx, commandID)
}

func (s *CommandService) applyStoredCommandConfig(ctx context.Context, commandID string) error {
	config, err := s.commandConfig(ctx, commandID)
	if err != nil || config == nil {
		return err
	}
	return s.applyCommandConfigRoots(*config)
}

func (s *CommandService) applyCommandConfigRoots(config storage.CommandConfigRecord) error {
	if s == nil || s.Registry() == nil || strings.TrimSpace(config.EffectiveName) == "" {
		return nil
	}
	var aliases []string
	if strings.TrimSpace(config.AliasesJSON) != "" {
		if err := json.Unmarshal([]byte(config.AliasesJSON), &aliases); err != nil {
			return err
		}
	}
	return s.Registry().UpdateRoots(config.CommandID, config.EffectiveName, aliases)
}

func applyCommandConfig(descriptor commandcore.CommandDescriptor, config *storage.CommandConfigRecord) commandcore.CommandDescriptor {
	if config == nil {
		return descriptor
	}
	if strings.TrimSpace(config.EffectiveName) != "" {
		descriptor.Name = strings.TrimSpace(config.EffectiveName)
	}
	if strings.TrimSpace(config.Permission) != "" {
		descriptor.Permission = commandcore.CommandPermission(config.Permission)
	}
	if strings.TrimSpace(config.OutputMode) != "" {
		descriptor.OutputMode = commandcore.CommandOutputMode(config.OutputMode)
	}
	return descriptor
}

func (s *CommandService) ensureCommandConfig(ctx context.Context, descriptor commandcore.CommandDescriptor) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return
	}
	existing, err := s.infra.DB.GetCommandConfig(ctx, descriptor.ID)
	if err != nil || existing != nil {
		return
	}
	aliases, _ := json.Marshal(descriptor.Aliases)
	_ = s.infra.DB.UpsertCommandConfig(ctx, storage.CommandConfigRecord{
		CommandID:     descriptor.ID,
		ProviderKind:  string(descriptor.ProviderKind),
		PluginID:      descriptor.PluginID,
		OriginalName:  descriptor.Name,
		EffectiveName: descriptor.Name,
		AliasesJSON:   string(aliases),
		Enabled:       true,
		Permission:    string(descriptor.Permission),
		OutputMode:    string(descriptor.OutputMode),
		ConfigJSON:    "{}",
	})
}

func pluginCommandDescriptor(manifest plugin.ManifestV2, command plugin.ManifestV2Command) commandcore.CommandDescriptor {
	name := strings.TrimSpace(command.RootName)
	if name == "" {
		name = strings.TrimSpace(manifest.ID) + "." + strings.TrimSpace(command.Name)
	}
	permission := commandcore.CommandPermission(command.Permission)
	if permission == "" {
		permission = commandcore.CommandPermissionMember
	}
	outputMode := commandcore.CommandOutputMode(command.OutputMode)
	if outputMode == "" {
		outputMode = commandcore.CommandOutputDirect
	}
	capabilities := make([]string, 0, len(manifest.Access.Capabilities))
	for _, capability := range manifest.Access.Capabilities {
		capabilities = append(capabilities, string(capability))
	}
	return commandcore.CommandDescriptor{
		ID:           "plugin." + manifest.ID + "." + command.Name,
		Name:         name,
		Aliases:      append([]string(nil), command.Aliases...),
		Summary:      command.Summary,
		Usage:        command.Usage,
		ProviderKind: commandcore.CommandProviderPlugin,
		PluginID:     manifest.ID,
		Handler:      command.Handler,
		Permission:   permission,
		Scope:        commandcore.CommandScopeOrigin,
		Capabilities: capabilities,
		OutputMode:   outputMode,
		TimeoutMS:    command.TimeoutMS,
	}
}

func descriptorHasCapability(descriptor commandcore.CommandDescriptor, capability string) bool {
	for _, candidate := range descriptor.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

func canonicalCommandName(descriptor commandcore.CommandDescriptor) string {
	if strings.HasPrefix(descriptor.ID, "builtin.") {
		return strings.TrimPrefix(descriptor.ID, "builtin.")
	}
	return descriptor.Name
}

func (s *CommandService) startSession(ctx context.Context, personaKey string) (string, error) {
	if engine := s.engine(); engine != nil {
		return engine.StartSession(ctx, personaKey)
	}
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	sessionID := uuid.NewString()
	if err := s.infra.DB.CreateSession(ctx, sessionID, personaKey); err != nil {
		return "", err
	}
	return sessionID, nil
}

func (s *CommandService) bindSession(ctx context.Context, origin conversation.Origin, personaKey string, sessionID string, isNew bool) error {
	if s == nil || s.conversation == nil || s.conversation.Bindings() == nil {
		return fmt.Errorf("conversation binding service is not configured")
	}
	_, err := s.conversation.Bindings().BindSession(ctx, origin, personaKey, sessionID, isNew)
	return err
}

func (s *CommandService) getSession(ctx context.Context, sessionID string) (*storage.SessionRecord, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	return s.infra.DB.GetSession(ctx, sessionID)
}

func (s *CommandService) latestMessageID(ctx context.Context, sessionID string) (string, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	messages, err := s.infra.DB.GetRecentMessages(ctx, sessionID, 1)
	if err != nil || len(messages) == 0 {
		return "", err
	}
	return messages[0].ID, nil
}

func (s *CommandService) stopRuns(req chat.CommandRequest) []string {
	if s == nil || s.conversation == nil || s.conversation.RunRegistry() == nil {
		return nil
	}
	s.conversation.RunRegistry().Stop(conversation.StopSelector{OriginKey: req.Origin.OriginKey, SessionID: req.SessionID})
	return nil
}

func (s *CommandService) rolloverMemory(ctx context.Context, sessionID string, personaKey string, reason string) []string {
	if s == nil || s.memory == nil {
		return nil
	}
	bridge := s.memory.Bridge()
	if bridge == nil {
		return nil
	}
	if _, err := bridge.RolloverSegment(ctx, sessionID, personaKey, reason); err != nil {
		if s.infra != nil && s.infra.Logger != nil {
			s.infra.Logger.Warn("command memory rollover failed", "session", sessionID, "reason", reason, "error", err)
		}
		return []string{"memory_rollover_failed"}
	}
	return nil
}

func (s *CommandService) engine() interface {
	StartSession(context.Context, string) (string, error)
	ResumeSession(context.Context, string, string) (string, bool, error)
} {
	if s == nil || s.chat == nil || s.chat.Engine() == nil {
		return nil
	}
	return s.chat.Engine()
}

func (s *CommandService) ensureOrigin(ctx context.Context, origin conversation.Origin) error {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	return s.infra.DB.UpsertConversationOrigin(ctx, storage.ConversationOriginRecord{
		OriginKey:    origin.OriginKey,
		SourceType:   firstNonEmptyCommandValue(origin.SourceType, conversation.DefaultSourceType),
		ChannelType:  firstNonEmptyCommandValue(origin.ChannelType, conversation.DefaultChannel),
		MetadataJSON: "{}",
	})
}

type commandExecution struct {
	service    *CommandService
	request    chat.CommandRequest
	parsed     commandcore.ParsedCommand
	descriptor commandcore.CommandDescriptor
	started    time.Time
}

type commandResult struct {
	Status        string
	Content       string
	ErrorKind     string
	SessionID     string
	ReloadHistory bool
	ReloadMemory  bool
	ContextSwitch string
	Payload       map[string]any
}

func (e commandExecution) finish(ctx context.Context, result commandResult) chat.CommandResponse {
	if result.Status == "" {
		result.Status = "success"
	}
	sessionID := firstNonEmptyCommandValue(result.SessionID, e.request.SessionID)
	payload := cloneCommandPayload(result.Payload)
	payload["command_id"] = e.descriptor.ID
	payload["command_name"] = e.parsed.Name
	payload["status"] = result.Status
	if result.ErrorKind != "" {
		payload["error_kind"] = result.ErrorKind
	}
	if result.ContextSwitch != "" {
		payload["reason"] = result.ContextSwitch
	}
	response := chat.CommandResponse{
		SessionID:  sessionID,
		PersonaKey: e.request.PersonaKey,
	}
	if result.ContextSwitch != "" {
		response.Messages = append(response.Messages, chat.WSMessage{
			Type:          "context_switched",
			Status:        result.Status,
			Content:       result.Content,
			SessionID:     sessionID,
			Persona:       e.request.PersonaKey,
			OriginKey:     e.request.Origin.OriginKey,
			ReloadHistory: result.ReloadHistory,
			ReloadMemory:  result.ReloadMemory,
			Payload:       payload,
		})
	}
	commandMsg := chat.WSMessage{
		Type:          "command_result",
		Status:        result.Status,
		Content:       result.Content,
		SessionID:     sessionID,
		Persona:       e.request.PersonaKey,
		OriginKey:     e.request.Origin.OriginKey,
		CommandID:     e.descriptor.ID,
		CommandName:   e.parsed.Name,
		ErrorKind:     result.ErrorKind,
		ReloadHistory: result.ReloadHistory,
		ReloadMemory:  result.ReloadMemory,
		Payload:       payload,
	}
	response.Messages = append(response.Messages, commandMsg)
	e.persist(ctx, result, sessionID, payload)
	return response
}

func (e commandExecution) persist(ctx context.Context, result commandResult, sessionID string, payload map[string]any) {
	if e.service == nil || e.service.infra == nil || e.service.infra.DB == nil {
		return
	}
	payloadJSON := mustCommandJSONObject(payload)
	argvJSON := mustCommandJSONArray(e.parsed.Args)
	flagsJSON := mustCommandJSONObject(e.parsed.Flags)
	_ = e.service.infra.DB.AddCommandInvocation(ctx, storage.CommandInvocationRecord{
		CommandID:    e.descriptor.ID,
		CommandName:  e.parsed.Name,
		ProviderKind: string(e.descriptor.ProviderKind),
		PluginID:     e.descriptor.PluginID,
		OriginKey:    e.request.Origin.OriginKey,
		SourceType:   e.request.Origin.SourceType,
		SessionID:    sessionID,
		PersonaKey:   e.request.PersonaKey,
		ActorRole:    e.request.ActorRole,
		InputHash:    commandInputHash(e.request.Content),
		ArgvJSON:     argvJSON,
		FlagsJSON:    flagsJSON,
		OutputMode:   string(e.descriptor.OutputMode),
		Status:       result.Status,
		ResultText:   result.Content,
		PayloadJSON:  payloadJSON,
		ErrorKind:    result.ErrorKind,
		DurationMS:   int(time.Since(e.started).Milliseconds()),
	})
	if e.service.conversation == nil || e.service.conversation.Timeline() == nil {
		return
	}
	if result.ContextSwitch != "" {
		_ = e.service.conversation.Timeline().Append(ctx, conversation.TimelineEvent{
			OriginKey:      e.request.Origin.OriginKey,
			SessionID:      sessionID,
			PersonaKey:     e.request.PersonaKey,
			Type:           "context_switched",
			VisibleContent: result.Content,
			PayloadJSON:    payloadJSON,
		})
	}
	_ = e.service.conversation.Timeline().Append(ctx, conversation.TimelineEvent{
		OriginKey:      e.request.Origin.OriginKey,
		SessionID:      sessionID,
		PersonaKey:     e.request.PersonaKey,
		Type:           "command_result",
		VisibleContent: result.Content,
		PayloadJSON:    payloadJSON,
	})
}

func unknownDescriptor(name string) commandcore.CommandDescriptor {
	return commandcore.CommandDescriptor{
		ID:           "unknown." + strings.TrimSpace(name),
		Name:         name,
		ProviderKind: commandcore.CommandProviderBuiltin,
		Permission:   commandcore.CommandPermissionMember,
		Scope:        commandcore.CommandScopeOrigin,
		OutputMode:   commandcore.CommandOutputDirect,
	}
}

func statusWithWarnings(warnings []string) string {
	if len(warnings) > 0 {
		return "success_with_warning"
	}
	return "success"
}

func commandStatusSuccessful(status string) bool {
	switch strings.TrimSpace(status) {
	case "", "success", "success_with_warning":
		return true
	default:
		return false
	}
}

func warningsPayload(warnings []string) map[string]any {
	if len(warnings) == 0 {
		return nil
	}
	return map[string]any{"warnings": warnings}
}

func summaryReportPayload(report contextutil.SummaryUpdateReport) map[string]any {
	return map[string]any{
		"attempted":            report.Attempted,
		"skipped":              report.Skipped,
		"skip_reason":          report.SkipReason,
		"delta_count":          report.DeltaCount,
		"covered_until_before": report.CoveredUntilBefore,
		"covered_until_after":  report.CoveredUntilAfter,
		"summary_model":        report.SummaryModel,
	}
}

func cloneCommandPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = value
	}
	return out
}

func commandInputHash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func mustCommandJSONObject(value any) string {
	if value == nil {
		return "{}"
	}
	payload, err := json.Marshal(value)
	if err != nil || !json.Valid(payload) || len(payload) == 0 || payload[0] != '{' {
		return "{}"
	}
	return string(payload)
}

func mustCommandJSONArray(value any) string {
	payload, err := json.Marshal(value)
	if err != nil || !json.Valid(payload) || len(payload) == 0 || payload[0] != '[' {
		return "[]"
	}
	return string(payload)
}

func firstNonEmptyCommandValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

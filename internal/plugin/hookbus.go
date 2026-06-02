package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type HookHandler func(context.Context, HookContext) (HookResult, error)

type RegisteredHook struct {
	PluginID      string
	Authorizer    *Authorizer
	Hook          HookName
	Mode          HookMode
	FailurePolicy FailurePolicy
	Priority      int
	Timeout       time.Duration
	TimeoutMS     int
	Handler       HookHandler
}

type HookBusConfig struct {
	DefaultTimeout time.Duration
	MaxTimeout     time.Duration
}

type HookBus struct {
	mu     sync.RWMutex
	config HookBusConfig
	audit  AuditSink
	hooks  map[HookName][]RegisteredHook
}

func NewHookBus(config HookBusConfig, audit AuditSink) *HookBus {
	if config.DefaultTimeout <= 0 {
		config.DefaultTimeout = 80 * time.Millisecond
	}
	if config.MaxTimeout <= 0 {
		config.MaxTimeout = time.Second
	}
	return &HookBus{
		config: config,
		audit:  audit,
		hooks:  make(map[HookName][]RegisteredHook),
	}
}

func (b *HookBus) Register(hook RegisteredHook) error {
	if b == nil {
		return fmt.Errorf("hook bus is nil")
	}
	if strings.TrimSpace(hook.PluginID) == "" {
		return fmt.Errorf("plugin_id is required")
	}
	if !KnownHook(hook.Hook) {
		return fmt.Errorf("unknown hook %q", hook.Hook)
	}
	if hook.Mode == "" {
		hook.Mode = HookModeObserve
	}
	if !KnownHookMode(hook.Mode) {
		return fmt.Errorf("unknown hook mode %q", hook.Mode)
	}
	if hook.FailurePolicy == "" {
		hook.FailurePolicy = FailurePolicyFailOpen
	}
	if !KnownFailurePolicy(hook.FailurePolicy) {
		return fmt.Errorf("unknown failure policy %q", hook.FailurePolicy)
	}
	if hook.Handler == nil {
		return fmt.Errorf("hook handler is required")
	}
	if hook.Timeout == 0 && hook.TimeoutMS > 0 {
		hook.Timeout = time.Duration(hook.TimeoutMS) * time.Millisecond
	}
	if hook.Timeout < 0 || hook.Timeout > b.config.MaxTimeout {
		return fmt.Errorf("hook timeout must be between 0 and %s", b.config.MaxTimeout)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hooks[hook.Hook] = append(b.hooks[hook.Hook], hook)
	sortHooks(b.hooks[hook.Hook])
	return nil
}

func (b *HookBus) Dispatch(ctx context.Context, hook HookName, hc HookContext) (HookResult, error) {
	if b == nil {
		return HookResult{}, nil
	}
	b.mu.RLock()
	registered := append([]RegisteredHook(nil), b.hooks[hook]...)
	b.mu.RUnlock()
	if len(registered) == 0 {
		return HookResult{}, nil
	}
	sortHooks(registered)

	combined := HookResult{}
	for i, reg := range registered {
		result, audit, err := b.invoke(ctx, hook, hc, reg, i+1)
		combined.Patches = append(combined.Patches, result.Patches...)
		combined.RejectedPatches = append(combined.RejectedPatches, result.RejectedPatches...)
		combined.Decisions = append(combined.Decisions, result.Decisions...)
		combined.Events = append(combined.Events, result.Events...)
		if combined.Annotations == nil && len(result.Annotations) > 0 {
			combined.Annotations = map[string]any{}
		}
		for key, value := range result.Annotations {
			combined.Annotations[key] = value
		}
		_ = b.recordAudit(ctx, audit)
		if err != nil && reg.FailurePolicy == FailurePolicyFailClosed {
			return combined, err
		}
	}
	combined.Patches, combined.RejectedPatches = mergePatches(combined.Patches, combined.RejectedPatches)
	return combined, nil
}

func (b *HookBus) invoke(ctx context.Context, hook HookName, hc HookContext, reg RegisteredHook, seq int) (result HookResult, audit InvocationAudit, err error) {
	timeout := reg.Timeout
	if timeout <= 0 {
		timeout = b.config.DefaultTimeout
	}
	inputHash := hookInputHash(hc)
	invocation := invocationID(reg.PluginID, hook, hc.Envelope.TurnID, hc.Envelope.Stage, seq, inputHash)
	audit = InvocationAudit{
		PluginID:     reg.PluginID,
		InvocationID: invocation,
		Hook:         hook,
		Stage:        hc.Envelope.Stage,
		Status:       "done",
		InputHash:    inputHash,
		StartedAt:    time.Now(),
	}
	hc.Envelope.InvocationID = invocation
	hc.Envelope.PluginID = reg.PluginID
	hc.Envelope.Hook = hook
	if !audit.StartedAt.IsZero() {
		hc.Envelope.Deadline = audit.StartedAt.Add(timeout)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		audit.DurationMS = time.Since(audit.StartedAt).Milliseconds()
		for i := range result.Patches {
			result.Patches[i].PluginID = reg.PluginID
			result.Patches[i].Priority = reg.Priority
		}
		if reg.Authorizer != nil {
			var denied []RejectedPatch
			result.Patches, denied = authorizePatches(reg.Authorizer, result.Patches)
			result.RejectedPatches = append(result.RejectedPatches, denied...)
			if len(denied) > 0 {
				audit.Status = "failed"
				audit.ErrorKind = ErrorKindPluginCapabilityDenied
				if reg.FailurePolicy == FailurePolicyFailClosed {
					err = &PluginError{Kind: ErrorKindPluginCapabilityDenied, Err: ErrCapabilityDenied}
				}
			}
		}
		audit.PatchCount = len(result.Patches)
		if recovered := recover(); recovered != nil {
			audit.Status = "failed"
			audit.ErrorKind = ErrorKindPluginHookFailed
			err = &PluginError{Kind: ErrorKindPluginHookFailed, Err: fmt.Errorf("panic: %v", recovered)}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			audit.Status = "failed"
			audit.ErrorKind = ErrorKindPluginHookTimeout
			err = &PluginError{Kind: ErrorKindPluginHookTimeout, Err: err}
		} else if err != nil {
			audit.Status = "failed"
			if audit.ErrorKind == "" {
				audit.ErrorKind = ErrorKindPluginHookFailed
			}
			err = &PluginError{Kind: audit.ErrorKind, Err: err}
		}
		if err != nil && reg.FailurePolicy == FailurePolicyFailOpen {
			err = nil
		}
	}()

	result, err = reg.Handler(runCtx, hc)
	if runCtx.Err() != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		err = runCtx.Err()
	}
	return result, audit, err
}

func authorizePatches(authorizer *Authorizer, patches []Patch) ([]Patch, []RejectedPatch) {
	if authorizer == nil || len(patches) == 0 {
		return patches, nil
	}
	accepted := make([]Patch, 0, len(patches))
	var rejected []RejectedPatch
	for _, patch := range patches {
		capability, required := capabilityForPatch(patch.Type)
		if required {
			if err := authorizer.Require(capability); err != nil {
				rejected = append(rejected, RejectedPatch{
					Patch:     patch,
					ErrorKind: ErrorKindPluginCapabilityDenied,
					Reason:    err.Error(),
				})
				continue
			}
		}
		accepted = append(accepted, patch)
	}
	return accepted, rejected
}

func capabilityForPatch(patchType PatchType) (Capability, bool) {
	switch patchType {
	case PatchToolRequireApproval:
		return CapabilityToolRequireApproval, true
	case PatchToolDowngradePermission:
		return CapabilityToolObserve, true
	case PatchOutboundDecorateText:
		return CapabilityOutboundDecorate, true
	case PatchOutboundAddPayload, PatchOutboundEmitSafeDebug:
		return CapabilityOutboundSafeDebug, true
	case PatchWorkAddConstraintHint, PatchWorkAddAcceptanceHint:
		return CapabilityWorkDispatchAnnotate, true
	case PatchMemoryAddQueryHint, PatchMemoryAddSafeBlock, PatchMemorySuppressBlock:
		return CapabilityMemoryReadSafe, true
	case PatchTurnAnnotate, PatchLLMAddSystemAppendix, PatchLLMAddToolHint:
		return CapabilityTurnAnnotate, true
	default:
		return "", false
	}
}

func hookInputHash(hc HookContext) string {
	switch {
	case hc.Tool != nil && hc.Tool.InputHash != "":
		return hc.Tool.InputHash
	case hc.Tool != nil && hc.Tool.ResultHash != "":
		return hc.Tool.ResultHash
	case hc.Outbound != nil && hc.Outbound.ContentHash != "":
		return hc.Outbound.ContentHash
	case hc.Turn.UserContentHash != "":
		return hc.Turn.UserContentHash
	default:
		return contentHash(fmt.Sprintf("%s|%s|%s|%s", hc.Envelope.Hook, hc.Envelope.TurnID, hc.Envelope.Stage, hc.Envelope.SessionID))
	}
}

func (b *HookBus) recordAudit(ctx context.Context, audit InvocationAudit) error {
	if b == nil || b.audit == nil || audit.InvocationID == "" {
		return nil
	}
	return b.audit.RecordInvocation(ctx, audit)
}

func mergePatches(patches []Patch, rejected []RejectedPatch) ([]Patch, []RejectedPatch) {
	if len(patches) == 0 {
		return nil, rejected
	}
	accepted := make([]Patch, 0, len(patches))
	replacePaths := map[string]Patch{}
	for _, patch := range patches {
		if patch.Operation == "" {
			patch.Operation = PatchOpAppend
		}
		if patch.Operation == PatchOpReplace {
			if first, exists := replacePaths[patch.Path]; exists {
				rejected = append(rejected, RejectedPatch{
					Patch:     patch,
					ErrorKind: ErrorKindPluginPatchConflict,
					Reason:    fmt.Sprintf("replace patch conflicts with %s", first.PluginID),
				})
				continue
			}
			replacePaths[patch.Path] = patch
		}
		accepted = append(accepted, patch)
	}
	return accepted, rejected
}

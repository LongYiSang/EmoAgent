package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/turn"
)

type RuntimeKind string
type Capability string
type HookName string
type HookMode string
type FailurePolicy string
type PatchType string
type PatchOperation string

const (
	RuntimeBuiltin RuntimeKind = "builtin"
	RuntimeProcess RuntimeKind = "process"
)

const (
	CapabilityTurnRead                Capability = "turn.read"
	CapabilityTurnAnnotate            Capability = "turn.annotate"
	CapabilityMemoryReadSafe          Capability = "memory.read.safe"
	CapabilityMemoryCandidateSubmit   Capability = "memory.candidate.submit"
	CapabilityMemoryForgetRequest     Capability = "memory.forget.request"
	CapabilityMemoryForgetDestructive Capability = "memory.forget.destructive"
	CapabilityWorkObserve             Capability = "work.observe"
	CapabilityWorkDispatchAnnotate    Capability = "work.dispatch.annotate"
	CapabilityApprovalObserve         Capability = "approval.observe"
	CapabilityOutboundDecorate        Capability = "outbound.decorate"
	CapabilityOutboundSafeDebug       Capability = "outbound.emit.safe_debug"
	CapabilityToolRegister            Capability = "tool.register"
	CapabilityToolObserve             Capability = "tool.observe"
	CapabilityToolRequireApproval     Capability = "tool.require_approval"
	CapabilityAgentAffectRead         Capability = "agent_affect.read"
	CapabilityAgentAffectReadReason   Capability = "agent_affect.read.reason"
	CapabilityAgentAffectEvaluate     Capability = "agent_affect.evaluate"
	CapabilityAgentAffectSubmit       Capability = "agent_affect.submit"
	CapabilityAgentAffectWriteDelta   Capability = "agent_affect.write_delta"
	CapabilityAgentAffectWriteTarget  Capability = "agent_affect.write_target"
	CapabilityAgentAffectConfigure    Capability = "agent_affect.configure"
	CapabilityAgentAffectObserve      Capability = "agent_affect.observe"
)

const (
	HookBeforeIngressNormalize    HookName = "before_ingress_normalize"
	HookAfterIngressNormalize     HookName = "after_ingress_normalize"
	HookBeforeMemoryPrepare       HookName = "before_memory_prepare"
	HookAfterMemoryPrepare        HookName = "after_memory_prepare"
	HookBeforeMemoryRetrieve      HookName = "before_memory_retrieve"
	HookAfterMemoryRetrieve       HookName = "after_memory_retrieve"
	HookBeforeMemoryCommit        HookName = "before_memory_commit"
	HookAfterMemoryCommit         HookName = "after_memory_commit"
	HookBeforeOutbound            HookName = "before_outbound"
	HookAfterOutbound             HookName = "after_outbound"
	HookBeforeToolCall            HookName = "before_tool_call"
	HookAfterToolCall             HookName = "after_tool_call"
	HookMemoryCandidateSubmit     HookName = "memory.candidate.submit"
	HookMemoryForgetRequest       HookName = "memory.forget.request"
	HookWorkDispatchAnnotate      HookName = "work.dispatch.annotate"
	HookOnDecisionPacket          HookName = "on_decision_packet"
	HookOnApprovalRequested       HookName = "on_approval_requested"
	HookOnApprovalResolved        HookName = "on_approval_resolved"
	HookOnApprovalConsumed        HookName = "on_approval_consumed"
	HookOnTurnError               HookName = "on_turn_error"
	HookAfterTurnEnd              HookName = "after_turn_end"
	HookBeforeAgentAffectEvaluate HookName = "before_agent_affect_evaluate"
	HookAfterAgentAffectEvaluate  HookName = "after_agent_affect_evaluate"
	HookBeforeAgentAffectCommit   HookName = "before_agent_affect_commit"
	HookAfterAgentAffectCommit    HookName = "after_agent_affect_commit"
	HookAgentAffectGetState       HookName = "agent_affect_get_state"
)

const (
	HookModeObserve    HookMode = "observe"
	HookModeTransform  HookMode = "transform"
	HookModeSideEffect HookMode = "side_effect"
)

const (
	FailurePolicyFailOpen   FailurePolicy = "fail_open"
	FailurePolicyFailClosed FailurePolicy = "fail_closed"
)

const (
	PatchTurnAnnotate            PatchType = "turn.annotate"
	PatchMemoryAddQueryHint      PatchType = "memory.add_query_hint"
	PatchMemoryAddSafeBlock      PatchType = "memory.add_safe_context_block"
	PatchMemorySuppressBlock     PatchType = "memory.suppress_context_block"
	PatchLLMAddSystemAppendix    PatchType = "llm.add_system_appendix"
	PatchLLMAddToolHint          PatchType = "llm.add_tool_hint"
	PatchOutboundDecorateText    PatchType = "outbound.decorate_text"
	PatchOutboundAddPayload      PatchType = "outbound.add_payload"
	PatchOutboundEmitSafeDebug   PatchType = "outbound.emit.safe_debug"
	PatchToolRequireApproval     PatchType = "tool.require_approval"
	PatchToolDowngradePermission PatchType = "tool.downgrade_permission"
	PatchWorkAddConstraintHint   PatchType = "work.add_constraint_hint"
	PatchWorkAddAcceptanceHint   PatchType = "work.add_acceptance_hint"
)

const (
	PatchOpAppend  PatchOperation = "append"
	PatchOpReplace PatchOperation = "replace"
	PatchOpSecure  PatchOperation = "secure"
)

const (
	ErrorKindPluginHookFailed       = "plugin_hook_failed"
	ErrorKindPluginHookTimeout      = "plugin_hook_timeout"
	ErrorKindPluginCapabilityDenied = "plugin_capability_denied"
	ErrorKindPluginPatchConflict    = "plugin_patch_conflict"
	ErrorKindPluginPolicyViolation  = "plugin_policy_violation"
)

var (
	ErrCapabilityDenied = errors.New("plugin capability denied")
	ErrPatchConflict    = errors.New("plugin patch conflict")
)

type Manifest struct {
	ID              string       `json:"id" yaml:"id"`
	Name            string       `json:"name" yaml:"name"`
	Version         string       `json:"version" yaml:"version"`
	Runtime         RuntimeKind  `json:"runtime" yaml:"runtime"`
	EmoAgentVersion string       `json:"emoagent_version" yaml:"emoagent_version"`
	Capabilities    []Capability `json:"capabilities" yaml:"capabilities"`
	Hooks           []HookSpec   `json:"hooks" yaml:"hooks"`
}

type HookSpec struct {
	Name          HookName      `json:"name" yaml:"name"`
	Mode          HookMode      `json:"mode" yaml:"mode"`
	FailurePolicy FailurePolicy `json:"failure_policy" yaml:"failure_policy"`
	Priority      int           `json:"priority" yaml:"priority"`
	TimeoutMS     int           `json:"timeout_ms" yaml:"timeout_ms"`
}

type ManifestValidationOptions struct {
	MaxTimeoutMS int
}

func (m Manifest) Validate(options ManifestValidationOptions) error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if !validPluginID(m.ID) {
		return fmt.Errorf("id %q is invalid", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !validSemver(m.Version) {
		return fmt.Errorf("version must be semantic version, got %q", m.Version)
	}
	if !validSemverRange(m.EmoAgentVersion) {
		return fmt.Errorf("emoagent_version must be a semver range, got %q", m.EmoAgentVersion)
	}
	switch m.Runtime {
	case RuntimeBuiltin, RuntimeProcess:
	default:
		return fmt.Errorf("runtime %q is unsupported", m.Runtime)
	}
	for _, capability := range m.Capabilities {
		if !KnownCapability(capability) {
			return fmt.Errorf("unknown capability %q", capability)
		}
	}
	maxTimeout := options.MaxTimeoutMS
	if maxTimeout <= 0 {
		maxTimeout = 1000
	}
	for i, hook := range m.Hooks {
		if !KnownHook(hook.Name) {
			return fmt.Errorf("hooks[%d]: unknown hook %q", i, hook.Name)
		}
		if !KnownHookMode(hook.Mode) {
			return fmt.Errorf("hooks[%d]: unknown mode %q", i, hook.Mode)
		}
		if !KnownFailurePolicy(hook.FailurePolicy) {
			return fmt.Errorf("hooks[%d]: unknown failure_policy %q", i, hook.FailurePolicy)
		}
		if hook.TimeoutMS < 0 || hook.TimeoutMS > maxTimeout {
			return fmt.Errorf("hooks[%d].timeout_ms must be between 0 and %d", i, maxTimeout)
		}
	}
	return nil
}

func KnownCapability(capability Capability) bool {
	switch capability {
	case CapabilityTurnRead,
		CapabilityTurnAnnotate,
		CapabilityMemoryReadSafe,
		CapabilityMemoryCandidateSubmit,
		CapabilityMemoryForgetRequest,
		CapabilityMemoryForgetDestructive,
		CapabilityWorkObserve,
		CapabilityWorkDispatchAnnotate,
		CapabilityApprovalObserve,
		CapabilityOutboundDecorate,
		CapabilityOutboundSafeDebug,
		CapabilityToolRegister,
		CapabilityToolObserve,
		CapabilityToolRequireApproval,
		CapabilityAgentAffectRead,
		CapabilityAgentAffectReadReason,
		CapabilityAgentAffectEvaluate,
		CapabilityAgentAffectSubmit,
		CapabilityAgentAffectWriteDelta,
		CapabilityAgentAffectWriteTarget,
		CapabilityAgentAffectConfigure,
		CapabilityAgentAffectObserve:
		return true
	default:
		return false
	}
}

func KnownHook(hook HookName) bool {
	switch hook {
	case HookBeforeIngressNormalize,
		HookAfterIngressNormalize,
		HookBeforeMemoryPrepare,
		HookAfterMemoryPrepare,
		HookBeforeMemoryRetrieve,
		HookAfterMemoryRetrieve,
		HookBeforeMemoryCommit,
		HookAfterMemoryCommit,
		HookBeforeOutbound,
		HookAfterOutbound,
		HookBeforeToolCall,
		HookAfterToolCall,
		HookMemoryCandidateSubmit,
		HookMemoryForgetRequest,
		HookWorkDispatchAnnotate,
		HookOnDecisionPacket,
		HookOnApprovalRequested,
		HookOnApprovalResolved,
		HookOnApprovalConsumed,
		HookOnTurnError,
		HookAfterTurnEnd,
		HookBeforeAgentAffectEvaluate,
		HookAfterAgentAffectEvaluate,
		HookBeforeAgentAffectCommit,
		HookAfterAgentAffectCommit,
		HookAgentAffectGetState:
		return true
	default:
		return false
	}
}

func KnownHookMode(mode HookMode) bool {
	switch mode {
	case HookModeObserve, HookModeTransform, HookModeSideEffect:
		return true
	default:
		return false
	}
}

func KnownFailurePolicy(policy FailurePolicy) bool {
	switch policy {
	case FailurePolicyFailOpen, FailurePolicyFailClosed:
		return true
	default:
		return false
	}
}

type Authorizer struct {
	pluginID     string
	capabilities map[Capability]struct{}
}

func NewAuthorizer(manifest Manifest) *Authorizer {
	capabilities := make(map[Capability]struct{}, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		capabilities[capability] = struct{}{}
	}
	return &Authorizer{pluginID: manifest.ID, capabilities: capabilities}
}

func (a *Authorizer) Require(capability Capability) error {
	if a == nil {
		return fmt.Errorf("%w: authorizer is nil", ErrCapabilityDenied)
	}
	if _, ok := a.capabilities[capability]; !ok {
		return fmt.Errorf("%w: plugin %s lacks %s", ErrCapabilityDenied, a.pluginID, capability)
	}
	return nil
}

func (a *Authorizer) Has(capability Capability) bool {
	return a != nil && a.Require(capability) == nil
}

type HookEnvelope struct {
	InvocationID string
	Hook         HookName
	PluginID     string
	TurnID       string
	Stage        turn.StageName
	State        turn.TurnState
	SessionID    string
	PersonaKey   string
	Traceparent  string
	Deadline     time.Time
	Capabilities []Capability
}

type HookContext struct {
	Envelope HookEnvelope
	Turn     TurnView
	Memory   *MemoryView
	Tool     *ToolCallView
	Work     *WorkView
	Outbound *OutboundView
	Config   map[string]any
}

type HookResult struct {
	Annotations     map[string]any
	Patches         []Patch
	RejectedPatches []RejectedPatch
	Decisions       []DecisionPatch
	Events          []PluginEvent
	Metrics         HookMetrics
}

type Patch struct {
	Type       PatchType
	Operation  PatchOperation
	Path       string
	Value      any
	ReasonCode string
	PluginID   string
	Priority   int
}

type RejectedPatch struct {
	Patch     Patch
	ErrorKind string
	Reason    string
}

type DecisionPatch struct {
	Type       string
	ReasonCode string
	Value      any
}

type PluginEvent struct {
	Type    string
	Payload map[string]any
}

type HookMetrics struct {
	DurationMS int64
}

type PluginError struct {
	Kind string
	Err  error
}

func (e *PluginError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Kind
	}
	return e.Kind + ": " + e.Err.Error()
}

func (e *PluginError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ErrorKind(err error) string {
	var pluginErr *PluginError
	if errors.As(err, &pluginErr) && pluginErr.Kind != "" {
		return pluginErr.Kind
	}
	return ErrorKindPluginHookFailed
}

func invocationID(pluginID string, hook HookName, turnID string, stage turn.StageName, seq int, inputHash string) string {
	parts := []string{pluginID, string(hook), turnID, string(stage), fmt.Sprintf("%d", seq), inputHash}
	return strings.Join(parts, ":")
}

func contentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validPluginID(id string) bool {
	ok, _ := regexp.MatchString(`^[a-z0-9][a-z0-9.-]*[a-z0-9]$`, id)
	return ok
}

func validSemver(version string) bool {
	ok, _ := regexp.MatchString(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`, strings.TrimSpace(version))
	return ok
}

func validSemverRange(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if validSemver(value) {
		return true
	}
	ok, _ := regexp.MatchString(`^(\^|~|>=|<=|>|<)\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`, value)
	return ok
}

func sortHooks(hooks []RegisteredHook) {
	sort.SliceStable(hooks, func(i, j int) bool {
		if hooks[i].Priority != hooks[j].Priority {
			return hooks[i].Priority < hooks[j].Priority
		}
		return hooks[i].PluginID < hooks[j].PluginID
	})
}

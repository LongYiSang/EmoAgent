package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
)

// SchemaValidator validates tool input against its JSON Schema.
type SchemaValidator interface {
	Validate(schema json.RawMessage, input json.RawMessage) error
}

// Dispatcher executes tool calls using the registry with validation and
// permission checks. Follows a fail-closed model: unknown tools, invalid
// input, and insufficient permissions all produce error results.
type Dispatcher struct {
	registry  *Registry
	validator SchemaValidator
	logger    *slog.Logger
	hook      CallHook
}

type CallHook interface {
	BeforeToolCall(context.Context, CallHookView) (CallHookDecision, error)
	AfterToolCall(context.Context, CallHookView, Result) error
}

type CallHookView struct {
	Call          Call
	Spec          Spec
	MaxPermission Permission
	AgentScope    Scope
}

type CallHookDecision struct {
	RequireApproval bool
	ApprovalKind    ApprovalKind
	ApprovalReason  string
	MaxPermission   Permission
	Reason          string
}

type CallAction string

const (
	CallActionExecute                      CallAction = "execute"
	CallActionError                        CallAction = "error"
	CallActionPermissionDenied             CallAction = "permission_denied"
	CallActionPermissionEscalationRequired CallAction = "permission_escalation_required"
	CallActionToolApprovalRequired         CallAction = "tool_approval_required"
)

type CallClassification struct {
	Call               Call
	Spec               Spec
	RequiredPermission Permission
	Action             CallAction
	Reason             string
	DestructiveReason  string
	ApprovalKind       ApprovalKind
	ApprovalReason     string
}

// NewDispatcher creates a Dispatcher. If a tool declares a schema, Execute
// requires a non-nil validator; otherwise the call is rejected.
func NewDispatcher(registry *Registry, validator SchemaValidator, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		registry:  registry,
		validator: validator,
		logger:    logger,
	}
}

func (d *Dispatcher) SetHook(hook CallHook) {
	if d != nil {
		d.hook = hook
	}
}

func (d *Dispatcher) ClassifyCall(ctx context.Context, call Call, maxPermission Permission) CallClassification {
	classification := CallClassification{
		Call:   call,
		Action: CallActionError,
	}
	if d == nil || d.registry == nil {
		classification.Reason = "tool dispatcher is not configured"
		return classification
	}

	spec, ok := d.registry.GetSpec(call.Name)
	if !ok {
		classification.Reason = fmt.Sprintf("tool %q not found", call.Name)
		return classification
	}
	classification.Spec = spec

	if _, ok := d.registry.Get(call.Name); !ok {
		classification.Reason = fmt.Sprintf("handler for tool %q not found", call.Name)
		return classification
	}

	if len(spec.Parameters) > 0 && d.validator == nil {
		classification.Reason = fmt.Sprintf("schema validation unavailable for tool %q", call.Name)
		return classification
	}
	if d.validator != nil && len(spec.Parameters) > 0 {
		if err := d.validator.Validate(spec.Parameters, call.Input); err != nil {
			classification.Reason = fmt.Sprintf("input validation failed: %v", err)
			return classification
		}
	}

	if d.hook != nil {
		decision, err := d.hook.BeforeToolCall(ctx, CallHookView{
			Call:          call,
			Spec:          spec,
			MaxPermission: maxPermission,
			AgentScope:    spec.Scope,
		})
		if err != nil {
			classification.Reason = fmt.Sprintf("tool hook rejected call: %v", err)
			return classification
		}
		if decision.RequireApproval {
			classification.Action = CallActionToolApprovalRequired
			classification.ApprovalKind = decision.ApprovalKind
			if classification.ApprovalKind == "" {
				classification.ApprovalKind = ApprovalKindDestructiveWrite
			}
			classification.ApprovalReason = decision.ApprovalReason
			classification.Reason = decision.Reason
			if classification.Reason == "" {
				classification.Reason = fmt.Sprintf("approval required by tool hook for %q", call.Name)
			}
			return classification
		}
		if decision.MaxPermission != "" && permissionLevel(decision.MaxPermission) >= 0 && permissionLevel(decision.MaxPermission) < permissionLevel(maxPermission) {
			maxPermission = decision.MaxPermission
		}
	}

	requiredPermission, destructiveReason := effectivePermission(spec, call.Input)
	classification.RequiredPermission = requiredPermission
	classification.DestructiveReason = destructiveReason
	if !PermissionSatisfied(maxPermission, requiredPermission) {
		if requiredPermission == PermApprovedDestructive && maxPermission == PermWorkspaceWrite {
			classification.Action = CallActionPermissionEscalationRequired
			classification.Reason = fmt.Sprintf(
				"permission escalation required: tool %q requires %q, granted %q",
				call.Name, requiredPermission, maxPermission,
			)
			return classification
		}
		classification.Action = CallActionPermissionDenied
		classification.Reason = fmt.Sprintf(
			"permission denied: tool %q requires %q, granted %q",
			call.Name, requiredPermission, maxPermission,
		)
		return classification
	}

	if requiredPermission == PermApprovedDestructive {
		classification.ApprovalKind = ApprovalKindDestructiveWrite
		classification.ApprovalReason = destructiveReason
		if ok, reason := approvalSatisfiedForCall(ctx, call, ApprovalKindDestructiveWrite); !ok {
			classification.Action = CallActionToolApprovalRequired
			classification.Reason = reason
			return classification
		}
	}

	if spec.ApprovalClassifier != nil {
		req, required := spec.ApprovalClassifier(ctx, call.Input)
		if required {
			classification.ApprovalKind = req.Kind
			classification.ApprovalReason = req.Reason
			if ok, reason := approvalSatisfiedForCall(ctx, call, req.Kind); !ok {
				classification.Action = CallActionToolApprovalRequired
				classification.Reason = reason
				return classification
			}
		}
	}

	classification.Action = CallActionExecute
	return classification
}

func approvalSatisfiedForCall(ctx context.Context, call Call, kind ApprovalKind) (bool, string) {
	approval, ok := ApprovalFromContext(ctx)
	if !ok || !approvalAllowsKind(approval, kind) {
		return false, fmt.Sprintf("approval required: tool %q needs an active approved request", call.Name)
	}
	binding, err := BuildApprovalBinding(call, approval.RequestID, kind)
	if err != nil {
		return false, fmt.Sprintf("approval required: failed to bind tool %q input: %v", call.Name, err)
	}
	if !approvalBindingMatches(approval, binding) {
		return false, fmt.Sprintf("approval binding mismatch: approved request does not match current tool call %q", call.Name)
	}
	return true, ""
}

func approvalAllowsKind(approval ApprovalContext, kind ApprovalKind) bool {
	if approval.AllowToolCall && approval.ApprovalKind == string(kind) {
		return true
	}
	return kind == ApprovalKindDestructiveWrite && approval.AllowDestructive &&
		(approval.ApprovalKind == "" || approval.ApprovalKind == string(ApprovalKindDestructiveWrite))
}

func approvalBindingMatches(approval ApprovalContext, binding ApprovalBinding) bool {
	if approval.ApprovalKind != binding.ApprovalKind {
		if !(binding.ApprovalKind == string(ApprovalKindDestructiveWrite) && approval.AllowDestructive && approval.ApprovalKind == "") {
			return false
		}
	}
	return approval.RequestID != "" &&
		approval.ToolName == binding.ToolName &&
		approval.NormalizedInputHash == binding.NormalizedInputHash &&
		approval.PathDigest == binding.PathDigest
}

// Execute runs a single tool call through the fail-closed pipeline:
//  1. Registry lookup → error if tool not found
//  2. Schema validation → error if input invalid or schema exists but no validator is configured
//  3. Permission check → error if maxPermission < tool's required permission
//  4. Handler execution → error if handler fails
func (d *Dispatcher) Execute(ctx context.Context, call Call, maxPermission Permission) Result {
	classification := d.ClassifyCall(ctx, call, maxPermission)
	return d.ExecuteClassified(ctx, classification, maxPermission)
}

func (d *Dispatcher) ExecuteClassified(ctx context.Context, classification CallClassification, maxPermission Permission) Result {
	call := classification.Call
	view := CallHookView{
		Call:          call,
		Spec:          classification.Spec,
		MaxPermission: maxPermission,
		AgentScope:    classification.Spec.Scope,
	}
	switch classification.Action {
	case CallActionExecute:
		// continue
	case CallActionToolApprovalRequired:
		d.logClassificationBlock(classification, maxPermission)
		return d.afterToolCall(ctx, view, approvalRequiredResult(call.ID, classification.Reason))
	case CallActionError, CallActionPermissionDenied, CallActionPermissionEscalationRequired:
		d.logClassificationBlock(classification, maxPermission)
		return d.afterToolCall(ctx, view, errorResult(call.ID, classification.Reason))
	default:
		return d.afterToolCall(ctx, view, errorResult(call.ID, fmt.Sprintf("unsupported tool action %q", classification.Action)))
	}

	if d == nil || d.registry == nil {
		return d.afterToolCall(ctx, view, errorResult(call.ID, "tool dispatcher is not configured"))
	}
	handler, _ := d.registry.Get(call.Name)
	if handler == nil {
		return d.afterToolCall(ctx, view, errorResult(call.ID, fmt.Sprintf("handler for tool %q not found", call.Name)))
	}

	// 4. Execute handler.
	d.logAttrs(slog.LevelInfo, "executing tool", "tool", call.Name, "call_id", call.ID)
	result, err := handler(ctx, call.Input)
	if err != nil {
		d.logAttrs(slog.LevelError, "tool execution failed",
			"tool", call.Name,
			"call_id", call.ID,
			"error", err,
		)
		return d.afterToolCall(ctx, view, errorResult(call.ID, fmt.Sprintf("execution error: %v", err)))
	}

	digest := contextutil.SnipToolResult(call.Name, call.ID, result, maxInt, maxInt)
	d.logAttrs(slog.LevelInfo, "tool executed",
		"tool", call.Name,
		"call_id", call.ID,
		"size", digest.Size,
		"preview", digest.Preview,
		"hash", digest.Hash,
	)
	return d.afterToolCall(ctx, view, Result{
		CallID:        call.ID,
		Content:       result,
		IsError:       false,
		NeedsApproval: false,
	})
}

func (d *Dispatcher) afterToolCall(ctx context.Context, view CallHookView, result Result) Result {
	if d == nil || d.hook == nil {
		return result
	}
	if err := d.hook.AfterToolCall(ctx, view, result); err != nil {
		d.logAttrs(slog.LevelWarn, "tool after hook failed", "tool", view.Call.Name, "call_id", view.Call.ID, "error", err)
	}
	return result
}

func effectivePermission(spec Spec, input json.RawMessage) (Permission, string) {
	if spec.Permission == PermWorkspaceWrite && spec.DestructiveClassifier != nil {
		if destructive, reason := spec.DestructiveClassifier(input); destructive {
			return PermApprovedDestructive, reason
		}
	}
	return spec.Permission, ""
}

// ExecuteAll runs multiple tool calls sequentially.
func (d *Dispatcher) ExecuteAll(ctx context.Context, calls []Call, maxPermission Permission) []Result {
	results := make([]Result, len(calls))
	for i, call := range calls {
		results[i] = d.Execute(ctx, call, maxPermission)
	}
	return results
}

func (d *Dispatcher) ExecuteAllClassified(ctx context.Context, classifications []CallClassification, maxPermission Permission) []Result {
	results := make([]Result, len(classifications))
	for i, classification := range classifications {
		results[i] = d.ExecuteClassified(ctx, classification, maxPermission)
	}
	return results
}

// ExtractToolCalls extracts Call values from a ChatResponse's ContentBlocks.
func ExtractToolCalls(resp *llm.ChatResponse) []Call {
	var calls []Call
	for _, block := range resp.ContentBlocks {
		if block.Type == "tool_use" {
			calls = append(calls, Call{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}
	return calls
}

// ResultsToMessages converts tool results into provider-specific LLM messages.
// Anthropic expects a single user message with tool_result content blocks.
// OpenAI expects one role=tool message per result with a tool_call_id.
func ResultsToMessages(provider string, results []Result) []llm.Message {
	switch provider {
	case "anthropic":
		blocks := make([]llm.ContentBlock, len(results))
		for i, r := range results {
			blocks[i] = llm.ContentBlock{
				Type:    "tool_result",
				ID:      r.CallID,
				Content: string(r.Content),
				IsError: r.IsError,
			}
		}
		return []llm.Message{{
			Role:          llm.RoleUser,
			ContentBlocks: blocks,
		}}
	case "openai":
		msgs := make([]llm.Message, len(results))
		for i, r := range results {
			msgs[i] = llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: r.CallID,
				Content:    string(r.Content),
			}
		}
		return msgs
	default:
		panic(fmt.Sprintf("unsupported provider %q", provider))
	}
}

func errorResult(callID, msg string) Result {
	errJSON, _ := json.Marshal(map[string]string{"error": msg})
	return Result{
		CallID:        callID,
		Content:       errJSON,
		IsError:       true,
		NeedsApproval: false,
	}
}

func approvalRequiredResult(callID, msg string) Result {
	result := errorResult(callID, msg)
	result.NeedsApproval = true
	return result
}

const maxInt = int(^uint(0) >> 1)

func (d *Dispatcher) logClassificationBlock(classification CallClassification, maxPermission Permission) {
	level := slog.LevelWarn
	if classification.Action == CallActionError {
		level = slog.LevelError
	}
	d.logAttrs(level, "tool classification blocked",
		"tool", classification.Call.Name,
		"call_id", classification.Call.ID,
		"action", classification.Action,
		"required", classification.RequiredPermission,
		"granted", maxPermission,
		"reason", classification.Reason,
	)
}

func (d *Dispatcher) logAttrs(level slog.Level, msg string, args ...any) {
	if d == nil || d.logger == nil {
		return
	}
	switch level {
	case slog.LevelDebug:
		d.logger.Debug(msg, args...)
	case slog.LevelInfo:
		d.logger.Info(msg, args...)
	case slog.LevelWarn:
		d.logger.Warn(msg, args...)
	case slog.LevelError:
		d.logger.Error(msg, args...)
	default:
		d.logger.Log(context.Background(), level, msg, args...)
	}
}

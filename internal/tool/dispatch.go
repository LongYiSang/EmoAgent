package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

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

// WouldNeedApproval reports whether the call should be intercepted for explicit
// approval before execution.
func (d *Dispatcher) WouldNeedApproval(ctx context.Context, call Call, maxPermission Permission) bool {
	if d == nil || d.registry == nil {
		return false
	}
	spec, ok := d.registry.GetSpec(call.Name)
	if !ok {
		return false
	}
	requiredPermission := effectivePermission(spec, call.Input)
	if requiredPermission != PermApprovedDestructive {
		return false
	}
	if len(spec.Parameters) > 0 && d.validator == nil {
		return false
	}
	if d.validator != nil && len(spec.Parameters) > 0 {
		if err := d.validator.Validate(spec.Parameters, call.Input); err != nil {
			return false
		}
	}
	if !PermissionSatisfied(maxPermission, requiredPermission) {
		return false
	}
	_, ok = ApprovalFromContext(ctx)
	return !ok
}

// Execute runs a single tool call through the fail-closed pipeline:
//  1. Registry lookup → error if tool not found
//  2. Schema validation → error if input invalid or schema exists but no validator is configured
//  3. Permission check → error if maxPermission < tool's required permission
//  4. Handler execution → error if handler fails
func (d *Dispatcher) Execute(ctx context.Context, call Call, maxPermission Permission) Result {
	// 1. Registry lookup.
	spec, ok := d.registry.GetSpec(call.Name)
	if !ok {
		d.logger.Warn("tool not found", "tool", call.Name, "call_id", call.ID)
		return errorResult(call.ID, fmt.Sprintf("tool %q not found", call.Name))
	}

	handler, ok := d.registry.Get(call.Name)
	if !ok {
		return errorResult(call.ID, fmt.Sprintf("handler for tool %q not found", call.Name))
	}

	// 2. Schema validation.
	if len(spec.Parameters) > 0 && d.validator == nil {
		d.logger.Error("schema validator missing",
			"tool", call.Name,
			"call_id", call.ID,
		)
		return errorResult(call.ID, fmt.Sprintf("schema validation unavailable for tool %q", call.Name))
	}
	if d.validator != nil && len(spec.Parameters) > 0 {
		if err := d.validator.Validate(spec.Parameters, call.Input); err != nil {
			d.logger.Warn("schema validation failed",
				"tool", call.Name,
				"call_id", call.ID,
				"error", err,
			)
			return errorResult(call.ID, fmt.Sprintf("input validation failed: %v", err))
		}
	}

	requiredPermission := effectivePermission(spec, call.Input)

	// 3. Permission check.
	if !PermissionSatisfied(maxPermission, requiredPermission) {
		d.logger.Warn("permission denied",
			"tool", call.Name,
			"call_id", call.ID,
			"required", requiredPermission,
			"granted", maxPermission,
		)
		return errorResult(call.ID, fmt.Sprintf(
			"permission denied: tool %q requires %q, granted %q",
			call.Name, requiredPermission, maxPermission,
		))
	}
	if requiredPermission == PermApprovedDestructive {
		approval, ok := ApprovalFromContext(ctx)
		if !ok || !approval.AllowDestructive {
			d.logger.Warn("approval guard denied",
				"tool", call.Name,
				"call_id", call.ID,
			)
			return approvalRequiredResult(call.ID, fmt.Sprintf("approval required: tool %q needs an active approved request", call.Name))
		}
	}

	// 4. Execute handler.
	d.logger.Info("executing tool", "tool", call.Name, "call_id", call.ID)
	result, err := handler(ctx, call.Input)
	if err != nil {
		d.logger.Error("tool execution failed",
			"tool", call.Name,
			"call_id", call.ID,
			"error", err,
		)
		return errorResult(call.ID, fmt.Sprintf("execution error: %v", err))
	}

	digest := contextutil.SnipToolResult(call.Name, call.ID, result, maxInt, maxInt)
	d.logger.Info("tool executed",
		"tool", call.Name,
		"call_id", call.ID,
		"size", digest.Size,
		"preview", digest.Preview,
		"hash", digest.Hash,
	)
	return Result{
		CallID:        call.ID,
		Content:       result,
		IsError:       false,
		NeedsApproval: false,
	}
}

func effectivePermission(spec Spec, input json.RawMessage) Permission {
	if spec.Name == "bash" && bashCommandRequiresApproval(input) {
		return PermApprovedDestructive
	}
	return spec.Permission
}

func bashCommandRequiresApproval(input json.RawMessage) bool {
	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return false
	}
	command := strings.ToLower(" " + payload.Command + " ")
	if command == "  " {
		return false
	}
	for _, needle := range []string{
		" git reset --hard",
		" git clean -",
		" git checkout --",
		" git restore --source",
		" remove-item ",
		" del ",
		" erase ",
		" rmdir ",
		" rd ",
		" rm ",
		" rm -",
		" cp -f ",
		" mv -f ",
		" copy /y ",
		" move /y ",
		" truncate ",
	} {
		if strings.Contains(command, needle) {
			return true
		}
	}
	return false
}

// ExecuteAll runs multiple tool calls sequentially.
func (d *Dispatcher) ExecuteAll(ctx context.Context, calls []Call, maxPermission Permission) []Result {
	results := make([]Result, len(calls))
	for i, call := range calls {
		results[i] = d.Execute(ctx, call, maxPermission)
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

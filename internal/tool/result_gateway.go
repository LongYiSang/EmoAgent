package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/tool/resultv2"
)

const providerResultMaxBytes = 64 * 1024

type ResultGateway struct {
	MaxProviderBytes int
	Now              func() time.Time
}

func DefaultResultGateway() ResultGateway {
	return ResultGateway{MaxProviderBytes: providerResultMaxBytes}
}

func (g ResultGateway) Wrap(result Result, call Call, spec Spec) Result {
	content := jsonData(result.Content)
	status := resultv2.StatusOK
	if result.NeedsApproval {
		status = resultv2.StatusApprovalRequired
	} else if result.IsError {
		status = resultv2.StatusError
	}
	inputHash, err := NormalizedInputHash(call.Input)
	if err != nil {
		inputHash = rawSHA256(call.Input)
	}
	now := time.Now().UTC()
	if g.Now != nil {
		now = g.Now().UTC()
	}
	source := normalizedSource(spec)
	env := cloneEnvelope(result.Envelope)
	if env == nil {
		env = &resultv2.ToolResultEnvelope{}
	}
	env.SchemaVersion = resultv2.SchemaVersion
	env.CallID = firstNonEmpty(result.CallID, call.ID)
	env.Status = status
	if len(env.StructuredContent) == 0 {
		env.StructuredContent = content
	} else {
		env.StructuredContent = jsonData(env.StructuredContent)
	}
	env.Provenance.ProducerKind = producerKindForSource(source.Kind)
	env.Provenance.ProducerID = source.ProducerID
	env.Provenance.ProducerVersion = source.ProducerVersion
	env.Provenance.ToolName = firstNonEmpty(spec.Name, call.Name, env.Provenance.ToolName)
	env.Provenance.InvocationID = invocationID(env.CallID, call.Name, inputHash)
	env.Provenance.InputHash = inputHash
	env.Provenance.OutputHash = resultOutputHash(env.StructuredContent)
	env.Provenance.RuntimeKind = source.RuntimeKind
	env.Provenance.GeneratedAt = now
	env.Labels = source.DefaultLabels
	applyContentDerivedLabels(env)
	applyExecutionDerivedLabels(env)
	if env.Metrics.InputBytes == 0 {
		env.Metrics.InputBytes = int64(len(call.Input))
	}
	if env.Metrics.OutputBytes == 0 {
		env.Metrics.OutputBytes = int64(len(result.Content))
	}
	if result.IsError {
		env.Error = &resultv2.ToolError{Code: "tool_error", Message: contextutil.SnipToolResult(firstNonEmpty(call.Name, spec.Name), result.CallID, result.Content, 64, 128).Preview}
	}
	result.Envelope = env
	return result
}

func (g ResultGateway) ProviderContent(result Result) json.RawMessage {
	if result.Envelope == nil {
		result = g.Wrap(result, Call{ID: result.CallID}, Spec{})
	}
	env := result.Envelope
	data := env.StructuredContent
	if len(data) == 0 {
		data = result.Content
	}
	data = jsonData(data)
	truncated := false
	maxBytes := g.MaxProviderBytes
	if maxBytes <= 0 {
		maxBytes = providerResultMaxBytes
	}
	if len(data) > maxBytes {
		digest := contextutil.SnipToolResult(env.Provenance.ToolName, env.CallID, data, 0, 0)
		compact, _ := json.Marshal(map[string]any{
			"preview":      digest.Preview,
			"hash":         digest.Hash,
			"size":         digest.Size,
			"is_truncated": true,
		})
		data = compact
		truncated = true
	}
	rendered, err := json.Marshal(struct {
		Data    json.RawMessage `json:"data"`
		EmoMeta providerMeta    `json:"_emo_meta"`
	}{
		Data: data,
		EmoMeta: providerMeta{
			SchemaVersion:        env.SchemaVersion,
			Origin:               env.Labels.Origin,
			Executor:             env.Labels.Executor,
			InstructionAuthority: env.Labels.InstructionAuthority,
			Integrity:            env.Labels.Integrity,
			Sensitivity:          env.Labels.Sensitivity,
			Freshness:            env.Labels.Freshness,
			ProducerKind:         env.Provenance.ProducerKind,
			ProducerID:           env.Provenance.ProducerID,
			RuntimeKind:          env.Provenance.RuntimeKind,
			ToolName:             env.Provenance.ToolName,
			InputHash:            env.Provenance.InputHash,
			OutputHash:           env.Provenance.OutputHash,
			InvocationID:         env.Provenance.InvocationID,
			Truncated:            truncated,
		},
	})
	if err != nil {
		return json.RawMessage(`{"data":{"error":"result gateway render failed"},"_emo_meta":{"origin":"system_generated","executor":"unknown","instruction_authority":"data_only","integrity":"unverified"}}`)
	}
	return rendered
}

type providerMeta struct {
	SchemaVersion        string `json:"schema_version,omitempty"`
	Origin               string `json:"origin"`
	Executor             string `json:"executor"`
	InstructionAuthority string `json:"instruction_authority"`
	Integrity            string `json:"integrity"`
	Sensitivity          string `json:"sensitivity,omitempty"`
	Freshness            string `json:"freshness,omitempty"`
	ProducerKind         string `json:"producer_kind,omitempty"`
	ProducerID           string `json:"producer_id,omitempty"`
	RuntimeKind          string `json:"runtime_kind,omitempty"`
	ToolName             string `json:"tool_name,omitempty"`
	InputHash            string `json:"input_hash,omitempty"`
	OutputHash           string `json:"output_hash,omitempty"`
	InvocationID         string `json:"invocation_id,omitempty"`
	Truncated            bool   `json:"truncated,omitempty"`
}

func resultOutputHash(content json.RawMessage) string {
	content = jsonData(content)
	var normalized any
	if err := json.Unmarshal(content, &normalized); err == nil {
		if raw, err := json.Marshal(normalized); err == nil {
			return rawSHA256(raw)
		}
	}
	return rawSHA256(content)
}

func normalizedSource(spec Spec) ToolSourceMetadata {
	source := spec.Source
	if source.Kind == "" {
		source.Kind = ToolSourceBuiltin
	}
	if strings.TrimSpace(source.ProducerID) == "" {
		source.ProducerID = "emoagent"
	}
	if strings.TrimSpace(source.RuntimeKind) == "" {
		source.RuntimeKind = resultv2.RuntimeHost
	}
	labels := source.DefaultLabels
	if labels.Executor == "" {
		labels.Executor = resultv2.ExecutorHostBuiltin
	}
	if labels.Origin == "" {
		labels.Origin = resultv2.OriginSystemGenerated
	}
	if labels.Integrity == "" {
		labels.Integrity = resultv2.IntegrityHostVerified
	}
	if labels.InstructionAuthority == "" {
		labels.InstructionAuthority = resultv2.InstructionDataOnly
	}
	source.DefaultLabels = labels
	return source
}

func producerKindForSource(kind ToolSourceKind) string {
	switch kind {
	case ToolSourcePlugin:
		return resultv2.ProducerPlugin
	case ToolSourceRemote:
		return resultv2.ProducerRemote
	default:
		return resultv2.ProducerBuiltin
	}
}

func invocationID(callID, toolName, inputHash string) string {
	sum := sha256.Sum256([]byte(callID + "\x00" + toolName + "\x00" + inputHash))
	return "inv_" + hex.EncodeToString(sum[:8])
}

func rawSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func jsonData(data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return json.RawMessage(`null`)
	}
	if json.Valid(data) {
		return append(json.RawMessage(nil), data...)
	}
	encoded, err := json.Marshal(string(data))
	if err != nil {
		return json.RawMessage(`null`)
	}
	return encoded
}

func cloneEnvelope(env *resultv2.ToolResultEnvelope) *resultv2.ToolResultEnvelope {
	if env == nil {
		return nil
	}
	clone := *env
	clone.StructuredContent = append(json.RawMessage(nil), env.StructuredContent...)
	clone.Content = append([]resultv2.ContentItem(nil), env.Content...)
	clone.Artifacts = append([]resultv2.ArtifactRef(nil), env.Artifacts...)
	clone.Redactions = append([]resultv2.Redaction(nil), env.Redactions...)
	clone.Provenance.GrantIDs = append([]string(nil), env.Provenance.GrantIDs...)
	clone.Provenance.Sources = append([]resultv2.SourceRef(nil), env.Provenance.Sources...)
	if env.Error != nil {
		errCopy := *env.Error
		clone.Error = &errCopy
	}
	return &clone
}

func applyContentDerivedLabels(env *resultv2.ToolResultEnvelope) {
	if env == nil || env.Labels.Origin != resultv2.OriginWorkspaceFile || len(env.StructuredContent) == 0 {
		return
	}
	var payload struct {
		PathScope string `json:"path_scope"`
	}
	if err := json.Unmarshal(env.StructuredContent, &payload); err != nil {
		return
	}
	if payload.PathScope == "external" {
		env.Labels.Origin = resultv2.OriginHostFile
	}
}

func applyExecutionDerivedLabels(env *resultv2.ToolResultEnvelope) {
	if env == nil || env.Provenance.ToolName != "bash" || len(env.StructuredContent) == 0 {
		return
	}
	var payload struct {
		ExecutionMode string `json:"execution_mode"`
		Unavailable   bool   `json:"unavailable"`
		Sandboxed     bool   `json:"sandboxed"`
		Unsafe        bool   `json:"unsafe"`
	}
	if err := json.Unmarshal(env.StructuredContent, &payload); err != nil {
		return
	}
	switch payload.ExecutionMode {
	case "managed_host":
		env.Provenance.RuntimeKind = resultv2.RuntimeManagedHostProcess
		env.Provenance.SandboxProfile = ""
		env.Labels.Executor = resultv2.ExecutorManagedHost
		env.Labels.Origin = resultv2.OriginSystemGenerated
		env.Labels.Integrity = resultv2.IntegrityHostVerified
		env.Labels.InstructionAuthority = resultv2.InstructionDataOnly
	case "sandbox":
		if payload.Unavailable || !payload.Sandboxed {
			env.Provenance.RuntimeKind = resultv2.RuntimeUnavailable
			env.Provenance.SandboxProfile = ""
			env.Labels.Executor = resultv2.ExecutorHostBuiltin
			env.Labels.Origin = resultv2.OriginSystemGenerated
			env.Labels.Integrity = resultv2.IntegrityHostVerified
			env.Labels.InstructionAuthority = resultv2.InstructionDataOnly
			return
		}
		env.Provenance.RuntimeKind = resultv2.RuntimeHostSandbox
		env.Provenance.SandboxProfile = "sandbox"
		env.Labels.Executor = resultv2.ExecutorHostBuiltin
		env.Labels.Origin = resultv2.OriginSystemGenerated
		env.Labels.Integrity = resultv2.IntegrityHostVerified
		env.Labels.InstructionAuthority = resultv2.InstructionDataOnly
	case "unsafe_host_exec":
		env.Provenance.RuntimeKind = resultv2.RuntimeHost
		env.Provenance.SandboxProfile = "unsafe_host_exec"
		env.Labels.Executor = resultv2.ExecutorHostBuiltin
		env.Labels.Origin = resultv2.OriginSystemGenerated
		env.Labels.Integrity = resultv2.IntegrityUnverified
		env.Labels.InstructionAuthority = resultv2.InstructionDataOnly
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

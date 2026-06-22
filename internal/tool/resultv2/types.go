package resultv2

import (
	"encoding/json"
	"time"
)

const SchemaVersion = "emoagent.tool_result.v0.3"

type Status string

const (
	StatusOK               Status = "ok"
	StatusError            Status = "error"
	StatusApprovalRequired Status = "approval_required"
	StatusCancelled        Status = "cancelled"
)

type ContentItem struct {
	Type string          `json:"type"`
	Text string          `json:"text,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

type ToolResultEnvelope struct {
	SchemaVersion     string           `json:"schema_version"`
	CallID            string           `json:"call_id"`
	Status            Status           `json:"status"`
	Content           []ContentItem    `json:"content,omitempty"`
	StructuredContent json.RawMessage  `json:"structured_content,omitempty"`
	Provenance        Provenance       `json:"provenance"`
	Labels            ContentLabels    `json:"labels"`
	Artifacts         []ArtifactRef    `json:"artifacts,omitempty"`
	Redactions        []Redaction      `json:"redactions,omitempty"`
	Metrics           ExecutionMetrics `json:"metrics,omitempty"`
	Error             *ToolError       `json:"error,omitempty"`
}

type Provenance struct {
	ProducerKind       string      `json:"producer_kind"`
	ProducerID         string      `json:"producer_id,omitempty"`
	ProducerVersion    string      `json:"producer_version,omitempty"`
	ToolName           string      `json:"tool_name,omitempty"`
	ToolVersion        string      `json:"tool_version,omitempty"`
	InvocationID       string      `json:"invocation_id"`
	InputHash          string      `json:"input_hash"`
	OutputHash         string      `json:"output_hash"`
	RuntimeKind        string      `json:"runtime_kind"`
	RuntimeInstanceID  string      `json:"runtime_instance_id,omitempty"`
	SandboxProfile     string      `json:"sandbox_profile,omitempty"`
	CodeDigest         string      `json:"code_digest,omitempty"`
	ImageDigest        string      `json:"image_digest,omitempty"`
	EffectiveGrantHash string      `json:"effective_grant_hash,omitempty"`
	GrantIDs           []string    `json:"grant_ids,omitempty"`
	Sources            []SourceRef `json:"sources,omitempty"`
	GeneratedAt        time.Time   `json:"generated_at"`
}

const (
	ProducerBuiltin  = "builtin"
	ProducerPlugin   = "plugin"
	ProducerHostFile = "host_file"
	ProducerWeb      = "web"
	ProducerMemory   = "memory"
	ProducerRemote   = "remote"
)

const (
	RuntimeHost               = "host"
	RuntimeManagedHostProcess = "managed_host_process"
	RuntimeManagedPython      = "managed_python_process"
	RuntimeUnavailable        = "unavailable"
	RuntimeHostSandbox        = "host_sandbox"
	RuntimeContainer          = "container"
	RuntimeProcessDev         = "process_dev"
	RuntimeRemote             = "remote"
)

type ContentLabels struct {
	Executor             string `json:"executor"`
	Origin               string `json:"origin"`
	Integrity            string `json:"integrity"`
	InstructionAuthority string `json:"instruction_authority"`
	Sensitivity          string `json:"sensitivity,omitempty"`
	Freshness            string `json:"freshness,omitempty"`
}

const (
	ExecutorHostBuiltin         = "host_builtin"
	ExecutorTrustedBuiltin      = "trusted_builtin"
	ExecutorManagedHost         = "managed_host_process"
	ExecutorManagedPythonPlugin = "managed_python_plugin"
	ExecutorLegacyProcessPlugin = "legacy_process_plugin"
	ExecutorRemoteService       = "remote_service"
	ExecutorUnknown             = "unknown"
)

const (
	OriginSystemGenerated = "system_generated"
	OriginUserInput       = "user_input"
	OriginWorkspaceFile   = "workspace_file"
	OriginHostFile        = "host_file"
	OriginMemoryAuthority = "memory_authority"
	OriginExternalWeb     = "external_web"
	OriginPluginGenerated = "plugin_generated"
	OriginModelGenerated  = "model_generated"
	OriginRemoteService   = "remote_service"
)

const (
	IntegrityHostVerified      = "host_verified"
	IntegrityHashVerified      = "hash_verified"
	IntegritySignatureVerified = "signature_verified"
	IntegrityUnverified        = "unverified"
	IntegrityConflicting       = "conflicting"
)

const (
	InstructionHostControl           = "host_control"
	InstructionUserAuthority         = "user_authority"
	InstructionDataOnly              = "data_only"
	InstructionUntrustedInstructions = "untrusted_instructions"
)

const (
	SensitivityPublic    = "public"
	SensitivityInternal  = "internal"
	SensitivityPrivate   = "private"
	SensitivitySensitive = "sensitive"
	SensitivitySecret    = "secret"
)

const (
	FreshnessLive    = "live"
	FreshnessCached  = "cached"
	FreshnessStale   = "stale"
	FreshnessUnknown = "unknown"
)

type SourceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Hash string `json:"hash,omitempty"`
	URI  string `json:"uri,omitempty"`
}

type ArtifactRef struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Hash string `json:"hash,omitempty"`
}

type Redaction struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
}

type ExecutionMetrics struct {
	DurationMS  int64 `json:"duration_ms,omitempty"`
	InputBytes  int64 `json:"input_bytes,omitempty"`
	OutputBytes int64 `json:"output_bytes,omitempty"`
}

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

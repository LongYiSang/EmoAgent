package resource

import "time"

type PrincipalKind string

const (
	PrincipalWorkTask        PrincipalKind = "work_task"
	PrincipalSession         PrincipalKind = "session"
	PrincipalPluginInstance  PrincipalKind = "plugin_instance"
	PrincipalSystemComponent PrincipalKind = "system_component"
)

type PrincipalRef struct {
	Kind PrincipalKind `json:"kind"`
	ID   string        `json:"id"`
}

type EffectKind string

const (
	EffectHostFSRead       EffectKind = "host.fs.read"
	EffectHostFSWrite      EffectKind = "host.fs.write"
	EffectHostFSDelete     EffectKind = "host.fs.delete"
	EffectProcessExec      EffectKind = "process.exec"
	EffectNetworkHTTP      EffectKind = "network.http"
	EffectProviderGenerate EffectKind = "provider.generate"
	EffectMemoryReadSafe   EffectKind = "memory.read.safe"
	EffectMemorySubmit     EffectKind = "memory.candidate.submit"
	EffectPluginStateWrite EffectKind = "plugin.state.write"
	EffectArtifactCreate   EffectKind = "artifact.create"
)

type Effect struct {
	Kind        EffectKind `json:"kind"`
	Resource    string     `json:"resource,omitempty"`
	Dynamic     bool       `json:"dynamic,omitempty"`
	Destructive bool       `json:"destructive,omitempty"`
	Sensitive   bool       `json:"sensitive,omitempty"`
}

type PolicyAction string

const (
	PolicyActionAllow PolicyAction = "allow"
	PolicyActionAsk   PolicyAction = "ask"
	PolicyActionDeny  PolicyAction = "deny"
)

type PolicyDecision struct {
	Action          PolicyAction       `json:"action"`
	ReasonCodes     []string           `json:"reason_codes,omitempty"`
	RequiredEffects []Effect           `json:"required_effects,omitempty"`
	RequiredGrants  []GrantRequirement `json:"required_grants,omitempty"`
	ApprovalKind    string             `json:"approval_kind,omitempty"`
	PolicyVersion   string             `json:"policy_version,omitempty"`
}

type GrantRequirement struct {
	Capability string           `json:"capability"`
	Resource   ResourceSelector `json:"resource"`
	Operations []string         `json:"operations"`
}

const (
	CapabilityHostFSRead  = "host.fs.read"
	CapabilityHostFSWrite = "host.fs.write"
)

const (
	OperationMetadata        = "metadata"
	OperationList            = "list"
	OperationSearch          = "search"
	OperationRead            = "read"
	OperationCopyToWorkspace = "copy_to_workspace"
	OperationStageChange     = "stage_change"
	OperationCreate          = "create"
	OperationOverwrite       = "overwrite"
	OperationMove            = "move"
	OperationDelete          = "delete"
	OperationMkdir           = "mkdir"
	OperationRmdir           = "rmdir"
	OperationExecute         = "execute"
)

type ResourceSelectorKind string

const (
	ResourceSelectorPath       ResourceSelectorKind = "path"
	ResourceSelectorAlias      ResourceSelectorKind = "alias"
	ResourceSelectorResourceID ResourceSelectorKind = "resource_id"
)

type ResourceSelector struct {
	Kind        ResourceSelectorKind `json:"kind"`
	ID          string               `json:"id,omitempty"`
	RootID      string               `json:"root_id,omitempty"`
	DisplayPath string               `json:"display_path,omitempty"`
	Path        string               `json:"path,omitempty"`
}

type GrantConstraints struct {
	Recursive      bool     `json:"recursive,omitempty"`
	MaxDepth       int      `json:"max_depth,omitempty"`
	MaxFiles       int      `json:"max_files,omitempty"`
	MaxBytes       int64    `json:"max_bytes,omitempty"`
	FollowSymlinks bool     `json:"follow_symlinks,omitempty"`
	AllowedTypes   []string `json:"allowed_types,omitempty"`
	ExpectedHash   string   `json:"expected_hash,omitempty"`
	ExpectedFileID string   `json:"expected_file_id,omitempty"`
	DomainPorts    []int    `json:"domain_ports,omitempty"`
}

type GrantLifetime string

const (
	GrantLifetimeOnce       GrantLifetime = "once"
	GrantLifetimeTask       GrantLifetime = "task"
	GrantLifetimeSession    GrantLifetime = "session"
	GrantLifetimePersistent GrantLifetime = "persistent"
)

type GrantStatus string

const (
	GrantStatusPending  GrantStatus = "pending"
	GrantStatusActive   GrantStatus = "active"
	GrantStatusConsumed GrantStatus = "consumed"
	GrantStatusRevoked  GrantStatus = "revoked"
	GrantStatusExpired  GrantStatus = "expired"
)

const (
	GrantIssuedByPolicy = "policy"
	GrantIssuedByUser   = "user"
	GrantIssuedByAdmin  = "admin"
)

type GrantEnvelope struct {
	ID              string           `json:"id"`
	Principal       PrincipalRef     `json:"principal"`
	Capability      string           `json:"capability"`
	Resource        ResourceSelector `json:"resource"`
	Operations      []string         `json:"operations"`
	Constraints     GrantConstraints `json:"constraints"`
	Lifetime        GrantLifetime    `json:"lifetime"`
	Status          GrantStatus      `json:"status"`
	ApprovalRequest string           `json:"approval_request,omitempty"`
	BindingHash     string           `json:"binding_hash"`
	IssuedBy        string           `json:"issued_by"`
	CreatedAt       time.Time        `json:"created_at"`
	ExpiresAt       *time.Time       `json:"expires_at,omitempty"`
}

type ResourceRef struct {
	ID                string `json:"id"`
	Provider          string `json:"provider"`
	DisplayPath       string `json:"display_path"`
	RootID            string `json:"root_id,omitempty"`
	CanonicalPath     string `json:"-"`
	CanonicalPathHash string `json:"canonical_path_hash"`
	ResourceType      string `json:"resource_type"`
	FileIdentity      string `json:"file_identity,omitempty"`
}

type GrantListFilter struct {
	Principal  *PrincipalRef `json:"principal,omitempty"`
	Status     GrantStatus   `json:"status,omitempty"`
	Capability string        `json:"capability,omitempty"`
}

type ChangeSetStatus string

const (
	ChangeSetStatusStaged          ChangeSetStatus = "staged"
	ChangeSetStatusApprovalPending ChangeSetStatus = "approval_pending"
	ChangeSetStatusApplying        ChangeSetStatus = "applying"
	ChangeSetStatusApplied         ChangeSetStatus = "applied"
	ChangeSetStatusConflict        ChangeSetStatus = "conflict"
	ChangeSetStatusFailed          ChangeSetStatus = "failed"
	ChangeSetStatusCancelled       ChangeSetStatus = "cancelled"
	ChangeSetStatusRestored        ChangeSetStatus = "restored"
)

type ChangeOperation string

const (
	ChangeOpCreateFile    ChangeOperation = "create_file"
	ChangeOpOverwriteFile ChangeOperation = "overwrite_file"
	ChangeOpMove          ChangeOperation = "move"
	ChangeOpDelete        ChangeOperation = "delete"
	ChangeOpMkdir         ChangeOperation = "mkdir"
	ChangeOpRmdir         ChangeOperation = "rmdir"
)

type ChangeSetRequest struct {
	Principal       PrincipalRef     `json:"principal,omitempty"`
	Operation       ChangeOperation  `json:"operation"`
	Path            string           `json:"path,omitempty"`
	ResourceID      string           `json:"resource_id,omitempty"`
	TargetPath      string           `json:"target_path,omitempty"`
	Content         string           `json:"content,omitempty"`
	StagingID       string           `json:"staging_id,omitempty"`
	PermanentDelete bool             `json:"permanent_delete,omitempty"`
	Recursive       bool             `json:"recursive,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
	Resource        ResourceSelector `json:"-"`
	TargetResource  ResourceSelector `json:"-"`
}

const (
	DeleteModeQuarantine = "quarantine"
	DeleteModePermanent  = "permanent"
)

type ChangeApplyOptions struct {
	DeleteMode        string `json:"delete_mode,omitempty"`
	Recursive         bool   `json:"recursive,omitempty"`
	ResourceID        string `json:"resource_id,omitempty"`
	CanonicalPathHash string `json:"canonical_path_hash,omitempty"`
	BaselineHash      string `json:"baseline_hash,omitempty"`
	BaselineFileID    string `json:"baseline_file_id,omitempty"`
}

type ChangeSet struct {
	ID                string          `json:"id"`
	Principal         PrincipalRef    `json:"principal,omitempty"`
	Status            ChangeSetStatus `json:"status"`
	Operation         ChangeOperation `json:"operation"`
	Source            ResourceRef     `json:"source,omitempty"`
	Target            ResourceRef     `json:"target,omitempty"`
	TargetDisplayPath string          `json:"target_display_path,omitempty"`
	BaselineHash      string          `json:"baseline_hash,omitempty"`
	BaselineFileID    string          `json:"baseline_file_id,omitempty"`
	ContentHash       string          `json:"content_hash,omitempty"`
	PlanHash          string          `json:"plan_hash"`
	StagingPath       string          `json:"-"`
	QuarantinePath    string          `json:"-"`
	Preview           ChangePreview   `json:"preview"`
	PermanentDelete   bool            `json:"permanent_delete,omitempty"`
	Recursive         bool            `json:"recursive,omitempty"`
	ErrorMessage      string          `json:"error_message,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	AppliedAt         *time.Time      `json:"applied_at,omitempty"`
}

type ChangePreview struct {
	Summary       string     `json:"summary"`
	Diff          string     `json:"diff,omitempty"`
	Binary        bool       `json:"binary,omitempty"`
	Bytes         int64      `json:"bytes,omitempty"`
	AffectedFiles int        `json:"affected_files,omitempty"`
	Ops           []ChangeOp `json:"ops,omitempty"`
}

type ChangeOp struct {
	Index       int             `json:"index"`
	Operation   ChangeOperation `json:"operation"`
	SourcePath  string          `json:"source_path,omitempty"`
	TargetPath  string          `json:"target_path,omitempty"`
	SourceHash  string          `json:"source_hash,omitempty"`
	TargetHash  string          `json:"target_hash,omitempty"`
	Bytes       int64           `json:"bytes,omitempty"`
	Description string          `json:"description,omitempty"`
}

type StagedResource struct {
	ID          string    `json:"id"`
	Path        string    `json:"-"`
	ContentHash string    `json:"content_hash"`
	Bytes       int64     `json:"bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

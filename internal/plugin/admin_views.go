package plugin

import (
	"errors"

	"github.com/longyisang/emoagent/internal/storage"
)

var (
	ErrPluginNotFound      = errors.New("plugin not found")
	ErrPluginAdminDisabled = errors.New("plugin admin is disabled")
)

type AdminPluginSummary struct {
	PluginID           string                     `json:"plugin_id"`
	Version            string                     `json:"version"`
	Name               string                     `json:"name"`
	RuntimeKind        RuntimeKind                `json:"runtime_kind"`
	AccessTier         AccessTier                 `json:"access_tier"`
	Capabilities       []Capability               `json:"capabilities"`
	Hooks              []HookSpec                 `json:"hooks"`
	Enabled            bool                       `json:"enabled"`
	RuntimeStatus      RuntimeStatus              `json:"runtime_status"`
	PackageDigest      string                     `json:"package_digest"`
	ManifestDigest     string                     `json:"manifest_digest"`
	SignatureStatus    string                     `json:"signature_status"`
	PublisherID        string                     `json:"publisher_id"`
	SourceType         string                     `json:"source_type"`
	SourceRef          string                     `json:"source_ref"`
	InstalledAt        string                     `json:"installed_at"`
	StorePath          string                     `json:"store_path"`
	StatePath          string                     `json:"state_path"`
	CachePath          string                     `json:"cache_path"`
	RunPath            string                     `json:"run_path"`
	WorkspacePath      string                     `json:"workspace_path"`
	ProviderUsageToday PluginProviderUsageSummary `json:"provider_usage_today"`
}

type PluginProviderUsageSummary struct {
	Count           int `json:"count"`
	ErrorCount      int `json:"error_count"`
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	EstimatedTokens int `json:"estimated_tokens"`
}

type AdminPluginInstallRequest struct {
	Path        string `json:"path"`
	InstalledBy string `json:"installed_by"`
}

type AdminGitHubInstallRequest struct {
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	Tag         string `json:"tag"`
	Asset       string `json:"asset"`
	InstalledBy string `json:"installed_by"`
}

type AdminPluginEnableRequest struct {
	Version       string `json:"version"`
	UserGrantJSON string `json:"user_grant_json"`
}

type AdminPluginLogs struct {
	PluginID   string `json:"plugin_id"`
	StderrTail string `json:"stderr_tail"`
}

type AdminPluginAccessEvents struct {
	Events []storage.PluginAccessEvent `json:"events"`
}

type AdminPluginProviderUsage struct {
	Usage []storage.PluginProviderUsage `json:"usage"`
}

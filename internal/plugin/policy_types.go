package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type PluginTrustLevel string
type ToolExposure string
type InvocationPolicy string

const (
	TrustBlocked           PluginTrustLevel = "blocked"
	TrustDeveloper         PluginTrustLevel = "developer"
	TrustUserTrusted       PluginTrustLevel = "user_trusted"
	TrustVerifiedPublisher PluginTrustLevel = "verified_publisher"
	TrustOfficial          PluginTrustLevel = "official"
)

const (
	ExposureHidden  ToolExposure = "hidden"
	ExposureWork    ToolExposure = "work"
	ExposureEmotion ToolExposure = "emotion"
)

const (
	InvocationAuto InvocationPolicy = "auto"
	InvocationAsk  InvocationPolicy = "ask"
	InvocationDeny InvocationPolicy = "deny"
)

const TrustAcknowledgementActionEnablePlugin = "enable_plugin"

type PluginTrustAcknowledgement struct {
	PluginID                string           `json:"plugin_id,omitempty"`
	Version                 string           `json:"version"`
	PackageDigest           string           `json:"package_digest"`
	ManifestDigest          string           `json:"manifest_digest"`
	SignatureStatus         string           `json:"signature_status"`
	PublisherID             string           `json:"publisher_id"`
	DefaultToolExposure     ToolExposure     `json:"default_tool_exposure,omitempty"`
	DefaultInvocationPolicy InvocationPolicy `json:"default_invocation_policy,omitempty"`
	TargetUserGrantHash     string           `json:"target_user_grant_hash"`
	DependencyLockDigest    string           `json:"dependency_lock_digest"`
	AckNonce                string           `json:"ack_nonce,omitempty"`
	AckIssuedAt             string           `json:"ack_issued_at,omitempty"`
	UserAction              string           `json:"user_action,omitempty"`
	Reasons                 []string         `json:"reasons"`
}

type PluginTrustReview struct {
	Required        bool                        `json:"required"`
	Reasons         []string                    `json:"reasons,omitempty"`
	Acknowledgement *PluginTrustAcknowledgement `json:"acknowledgement,omitempty"`
}

type PluginTrustAcceptance struct {
	TrustLevel              PluginTrustLevel `json:"trust_level,omitempty"`
	AcceptedAt              string           `json:"accepted_at,omitempty"`
	AcknowledgementHash     string           `json:"acknowledgement_hash,omitempty"`
	Reasons                 []string         `json:"reasons,omitempty"`
	DefaultToolExposure     ToolExposure     `json:"default_tool_exposure,omitempty"`
	DefaultInvocationPolicy InvocationPolicy `json:"default_invocation_policy,omitempty"`
}

type PluginTrustSubject struct {
	PluginID                string
	Version                 string
	PackageDigest           string
	ManifestDigest          string
	SignatureStatus         string
	PublisherID             string
	RuntimeKind             RuntimeKind
	Capabilities            []Capability
	Hooks                   []HookSpec
	DefaultToolExposure     ToolExposure
	DefaultInvocationPolicy InvocationPolicy
}

type PluginHostAPIPolicySummary struct {
	ManifestCapabilities    []Capability `json:"manifest_capabilities,omitempty"`
	UserGrantedCapabilities []Capability `json:"user_granted_capabilities,omitempty"`
	HostAllowedCapabilities []Capability `json:"host_allowed_capabilities,omitempty"`
	EffectiveCapabilities   []Capability `json:"effective_capabilities,omitempty"`
	HostPolicyMode          string       `json:"host_policy_mode"`
}

type PluginToolPolicySummary struct {
	DefaultExposure   ToolExposure            `json:"default_exposure"`
	DefaultInvocation InvocationPolicy        `json:"default_invocation"`
	RegisteredTools   []PluginToolPolicyEntry `json:"registered_tools,omitempty"`
}

type PluginToolPolicyEntry struct {
	Name                   string           `json:"name"`
	HostExposure           ToolExposure     `json:"host_exposure"`
	HostInvocation         InvocationPolicy `json:"host_invocation"`
	SelfReportedScope      string           `json:"self_reported_scope,omitempty"`
	SelfReportedPermission string           `json:"self_reported_permission,omitempty"`
}

type PluginHookPolicySummary struct {
	AllowActiveHooks bool       `json:"allow_active_hooks"`
	ObserveHooks     []HookName `json:"observe_hooks,omitempty"`
	ActiveHooks      []HookName `json:"active_hooks,omitempty"`
}

func TrustLevelForSignatureStatus(signatureStatus string, blocked bool) PluginTrustLevel {
	return TrustLevelForInstall(signatureStatus, "", false, blocked)
}

func TrustLevelForInstall(signatureStatus, sourceType string, accepted bool, blocked bool) PluginTrustLevel {
	if blocked {
		return TrustBlocked
	}
	switch strings.TrimSpace(signatureStatus) {
	case SignatureStatusVerified:
		return TrustVerifiedPublisher
	case SignatureStatusUnsignedDev:
		return TrustDeveloper
	default:
		if accepted {
			return TrustUserTrusted
		}
		return TrustDeveloper
	}
}

func BuildPluginTrustReview(previous, target PluginTrustSubject) PluginTrustReview {
	if strings.TrimSpace(previous.PluginID) == "" || strings.TrimSpace(previous.Version) == "" {
		return PluginTrustReview{}
	}
	if previous.PluginID == target.PluginID && previous.Version == target.Version {
		return PluginTrustReview{}
	}
	reasons := pluginTrustReviewReasons(previous, target)
	if len(reasons) == 0 {
		return PluginTrustReview{}
	}
	ack := BuildPluginTrustAcknowledgement(target, reasons)
	return PluginTrustReview{Required: true, Reasons: reasons, Acknowledgement: &ack}
}

func BuildPluginTrustAcknowledgement(subject PluginTrustSubject, reasons []string) PluginTrustAcknowledgement {
	return PluginTrustAcknowledgement{
		PluginID:                subject.PluginID,
		Version:                 subject.Version,
		PackageDigest:           subject.PackageDigest,
		ManifestDigest:          subject.ManifestDigest,
		SignatureStatus:         subject.SignatureStatus,
		PublisherID:             subject.PublisherID,
		DefaultToolExposure:     subject.DefaultToolExposure,
		DefaultInvocationPolicy: subject.DefaultInvocationPolicy,
		Reasons:                 NormalizePluginTrustReasons(reasons),
	}
}

func NormalizePluginTrustReasons(reasons []string) []string {
	if reasons == nil {
		return []string{}
	}
	return append([]string(nil), reasons...)
}

func ValidatePluginTrustAcknowledgement(review PluginTrustReview, ack *PluginTrustAcknowledgement) error {
	if !review.Required {
		return nil
	}
	if ack == nil {
		return fmt.Errorf("plugin trust review required")
	}
	want := review.Acknowledgement
	if want == nil ||
		ack.PluginID != want.PluginID ||
		ack.Version != want.Version ||
		ack.PackageDigest != want.PackageDigest ||
		ack.ManifestDigest != want.ManifestDigest ||
		ack.SignatureStatus != want.SignatureStatus ||
		ack.PublisherID != want.PublisherID ||
		ack.DefaultToolExposure != want.DefaultToolExposure ||
		ack.DefaultInvocationPolicy != want.DefaultInvocationPolicy ||
		ack.TargetUserGrantHash != want.TargetUserGrantHash ||
		ack.DependencyLockDigest != want.DependencyLockDigest ||
		ack.AckNonce != want.AckNonce ||
		ack.AckIssuedAt != want.AckIssuedAt ||
		ack.UserAction != want.UserAction ||
		!sameStringSlice(ack.Reasons, want.Reasons) {
		return fmt.Errorf("plugin trust review acknowledgement does not match target")
	}
	return nil
}

func HashPluginTrustAcknowledgement(ack PluginTrustAcknowledgement) (string, error) {
	payload := struct {
		Schema          string                     `json:"schema"`
		Acknowledgement PluginTrustAcknowledgement `json:"acknowledgement"`
	}{
		Schema:          "emoagent.plugin.trust_ack.v1",
		Acknowledgement: ack,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func HashUserGrant(grant UserGrant) (string, error) {
	capabilities := uniqueSortedCapabilities(grant.Capabilities)
	payload := struct {
		Schema       string   `json:"schema"`
		Tier         string   `json:"tier"`
		Capabilities []string `json:"capabilities"`
	}{
		Schema:       "emoagent.plugin.user_grant.v1",
		Tier:         string(grant.Tier),
		Capabilities: capabilities,
	}
	return hashJSONPayload(payload)
}

type PluginHostPolicyFingerprintSubject struct {
	FacadePolicy            FacadeHostPolicy
	AllowActiveHooks        bool
	DefaultToolExposure     ToolExposure
	DefaultInvocationPolicy InvocationPolicy
}

func FingerprintPluginHostPolicy(subject PluginHostPolicyFingerprintSubject) (string, error) {
	capabilities := uniqueSortedCapabilities(subject.FacadePolicy.AllowedCapabilities)
	mode := "allow_all"
	if len(capabilities) > 0 {
		mode = "allow_list"
	}
	payload := struct {
		Schema              string           `json:"schema"`
		Mode                string           `json:"mode"`
		AllowedCapabilities []string         `json:"allowed_capabilities"`
		AllowActiveHooks    bool             `json:"allow_active_hooks"`
		DefaultToolExposure ToolExposure     `json:"default_tool_exposure"`
		DefaultInvocation   InvocationPolicy `json:"default_invocation_policy"`
	}{
		Schema:              "emoagent.plugin.host_policy.v1",
		Mode:                mode,
		AllowedCapabilities: capabilities,
		AllowActiveHooks:    subject.AllowActiveHooks,
		DefaultToolExposure: subject.DefaultToolExposure,
		DefaultInvocation:   subject.DefaultInvocationPolicy,
	}
	return hashJSONPayload(payload)
}

func uniqueSortedCapabilities(capabilities []Capability) []string {
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		value := strings.TrimSpace(string(capability))
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	values := make([]string, 0, len(seen))
	for value := range seen {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func hashJSONPayload(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func pluginTrustReviewReasons(previous, target PluginTrustSubject) []string {
	reasons := []string{}
	if strings.TrimSpace(previous.PublisherID) != strings.TrimSpace(target.PublisherID) {
		reasons = append(reasons, "publisher_changed")
	}
	if strings.TrimSpace(previous.SignatureStatus) != strings.TrimSpace(target.SignatureStatus) {
		reasons = append(reasons, "signature_status_changed")
	}
	if previous.RuntimeKind != "" && target.RuntimeKind != "" && previous.RuntimeKind != target.RuntimeKind {
		reasons = append(reasons, "runtime_kind_changed")
	}
	previousCapabilities := map[Capability]struct{}{}
	for _, capability := range previous.Capabilities {
		previousCapabilities[capability] = struct{}{}
	}
	var addedCapabilities []string
	for _, capability := range target.Capabilities {
		if _, ok := previousCapabilities[capability]; !ok {
			addedCapabilities = append(addedCapabilities, "capability_added:"+string(capability))
		}
	}
	sort.Strings(addedCapabilities)
	reasons = append(reasons, addedCapabilities...)

	previousActiveHooks := map[string]struct{}{}
	for _, hook := range previous.Hooks {
		if pluginTrustActiveHookMode(hook.Mode) {
			previousActiveHooks[activeHookReviewKey(hook)] = struct{}{}
		}
	}
	var addedActiveHooks []string
	for _, hook := range target.Hooks {
		if !pluginTrustActiveHookMode(hook.Mode) {
			continue
		}
		key := activeHookReviewKey(hook)
		if _, ok := previousActiveHooks[key]; !ok {
			addedActiveHooks = append(addedActiveHooks, "active_hook_added:"+key)
		}
	}
	sort.Strings(addedActiveHooks)
	reasons = append(reasons, addedActiveHooks...)
	if toolExposureAddsEmotion(previous.DefaultToolExposure, target.DefaultToolExposure) {
		reasons = append(reasons, "tool_exposure_added:emotion")
	}
	if invocationEnablesAuto(previous.DefaultInvocationPolicy, target.DefaultInvocationPolicy) {
		reasons = append(reasons, "tool_invocation_auto_enabled")
	}
	return reasons
}

func toolExposureAddsEmotion(previous, target ToolExposure) bool {
	return previous != "" && target == ExposureEmotion && previous != ExposureEmotion
}

func invocationEnablesAuto(previous, target InvocationPolicy) bool {
	return previous != "" && target == InvocationAuto && previous != InvocationAuto
}

func pluginTrustActiveHookMode(mode HookMode) bool {
	return mode == HookModeTransform || mode == HookModeSideEffect
}

func activeHookReviewKey(hook HookSpec) string {
	return string(hook.Name) + ":" + string(hook.Mode)
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/storage"
)

type FacadeBroker struct {
	storage  PluginFacadeStorage
	provider *ProviderGateway
	store    *PluginStore

	manifests map[string]ManifestV2
}

type PluginFacadeStorage interface {
	GetPluginEnabledState(context.Context, string) (*storage.PluginEnabledState, error)
	RecordPluginAccessEvent(context.Context, storage.PluginAccessEvent) error
	PluginKVGet(context.Context, string, string) (string, bool, error)
	PluginKVSet(context.Context, string, string, string) error
}

func NewFacadeBroker(storage PluginFacadeStorage, provider *ProviderGateway) *FacadeBroker {
	return &FacadeBroker{storage: storage, provider: provider, manifests: map[string]ManifestV2{}}
}

func (b *FacadeBroker) SetStore(store *PluginStore) {
	if b != nil {
		b.store = store
	}
}

func (b *FacadeBroker) AddPlugin(manifest ManifestV2) {
	if b == nil {
		return
	}
	b.manifests[manifest.ID] = manifest
	if b.provider != nil {
		b.provider.AddPlugin(manifest)
	}
}

func (b *FacadeBroker) Call(ctx context.Context, pluginID string, method string, params json.RawMessage) (json.RawMessage, error) {
	start := time.Now()
	capability, requiresCapability := capabilityForFacadeMethod(method)
	status := "allowed"
	var output json.RawMessage
	err := b.authorize(ctx, pluginID, capability, requiresCapability)
	if err == nil {
		output, err = b.dispatch(ctx, pluginID, method, params)
	}
	if err != nil {
		status = "denied"
	}
	_ = b.recordAccess(ctx, storage.PluginAccessEvent{
		PluginID:       pluginID,
		AccessKind:     method,
		Capability:     string(capability),
		Status:         status,
		RequestSummary: summarizeFacadeRequest(method, params),
		InputHash:      contentHash(string(params)),
		OutputHash:     contentHash(string(output)),
		DurationMS:     time.Since(start).Milliseconds(),
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

func (b *FacadeBroker) authorize(ctx context.Context, pluginID string, capability Capability, requiresCapability bool) error {
	if b == nil {
		return fmt.Errorf("facade broker is nil")
	}
	manifest, ok := b.manifests[pluginID]
	if !ok {
		return fmt.Errorf("plugin %q is not registered", pluginID)
	}
	if b.storage == nil {
		return fmt.Errorf("plugin storage is not configured")
	}
	state, err := b.storage.GetPluginEnabledState(ctx, pluginID)
	if err != nil {
		return err
	}
	if state == nil || !state.Enabled {
		return fmt.Errorf("plugin %q is not enabled", pluginID)
	}
	if requiresCapability {
		if err := NewAuthorizer(manifest.CompatManifest()).Require(capability); err != nil {
			return err
		}
		if err := grantAllows(state.UserGrantJSON, manifest.Access.Tier, capability); err != nil {
			return err
		}
	}
	return nil
}

func (b *FacadeBroker) dispatch(ctx context.Context, pluginID string, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "plugin.info":
		manifest := b.manifests[pluginID]
		return marshalRaw(map[string]any{
			"id":           manifest.ID,
			"name":         manifest.Name,
			"version":      manifest.Version,
			"access_tier":  manifest.Access.Tier,
			"capabilities": manifest.Access.Capabilities,
		})
	case "plugin.kv.get":
		var req struct {
			Key string `json:"key"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Key) == "" {
			return nil, fmt.Errorf("key is required")
		}
		value, found, err := b.storage.PluginKVGet(ctx, pluginID, req.Key)
		if err != nil {
			return nil, err
		}
		raw := json.RawMessage("null")
		if found {
			raw = json.RawMessage(value)
		}
		return marshalRaw(map[string]any{"found": found, "value": raw})
	case "plugin.kv.set":
		var req struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Key) == "" {
			return nil, fmt.Errorf("key is required")
		}
		if len(req.Value) == 0 {
			req.Value = json.RawMessage("null")
		}
		if !json.Valid(req.Value) {
			return nil, fmt.Errorf("value must be valid JSON")
		}
		if err := b.storage.PluginKVSet(ctx, pluginID, req.Key, string(req.Value)); err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"ok": true})
	case "plugin.files.read_text":
		var req struct {
			Path     string `json:"path"`
			MaxBytes int    `json:"max_bytes"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		path, err := b.pluginFilePath(pluginID, req.Path)
		if err != nil {
			return nil, err
		}
		limit := req.MaxBytes
		if limit <= 0 || limit > 256*1024 {
			limit = 256 * 1024
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(data) > limit {
			return nil, fmt.Errorf("file exceeds max_bytes")
		}
		return marshalRaw(map[string]any{"content": string(data)})
	case "plugin.files.write_text":
		var req struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		path, err := b.pluginFilePath(pluginID, req.Path)
		if err != nil {
			return nil, err
		}
		if len(req.Content) > 256*1024 {
			return nil, fmt.Errorf("content exceeds max size")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(req.Content), 0o644); err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"ok": true})
	case "memory.safe_context.current":
		var req struct {
			Scope string `json:"scope"`
			Limit int    `json:"limit"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"blocks": []any{}, "summary": ""})
	case "memory.candidate.submit":
		var req struct {
			Candidate json.RawMessage `json:"candidate"`
			Reason    string          `json:"reason"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"status": "queued"})
	case "memory.forget.request":
		var req struct {
			Query  string `json:"query"`
			Reason string `json:"reason"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"status": "requested"})
	case "work.decision.observe":
		var req struct {
			DecisionID string          `json:"decision_id"`
			Metadata   json.RawMessage `json:"metadata"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"ok": true})
	case "work.dispatch.annotate":
		var req struct {
			TaskID     string          `json:"task_id"`
			Annotation json.RawMessage `json:"annotation"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"ok": true})
	case "approval.observe":
		var req struct {
			RequestID string `json:"request_id"`
			Status    string `json:"status"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"ok": true})
	case "agent_affect.current":
		var req struct {
			PersonaID string `json:"persona_id"`
			SessionID string `json:"session_id"`
			View      string `json:"view"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		return marshalRaw(map[string]any{"ok": true})
	case "log.emit":
		var req struct {
			Level   string         `json:"level"`
			Message string         `json:"message"`
			Fields  map[string]any `json:"fields"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Message) == "" {
			return nil, fmt.Errorf("message is required")
		}
		return marshalRaw(map[string]any{"ok": true})
	case "metric.emit":
		var req struct {
			Name   string         `json:"name"`
			Value  float64        `json:"value"`
			Fields map[string]any `json:"fields"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Name) == "" {
			return nil, fmt.Errorf("name is required")
		}
		return marshalRaw(map[string]any{"ok": true})
	case "web.search":
		var req struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("web.search facade is reserved but not implemented")
	case "web.fetch":
		var req struct {
			URL string `json:"url"`
		}
		if err := decodeFacadeParams(params, &req); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("web.fetch facade is reserved but not implemented")
	case "provider.generate":
		if b.provider == nil {
			return nil, fmt.Errorf("provider gateway is not configured")
		}
		resp, err := b.provider.GenerateRaw(ctx, pluginID, params)
		if err != nil {
			return nil, err
		}
		return resp, nil
	default:
		return nil, fmt.Errorf("unsupported facade method %q", method)
	}
}

func (b *FacadeBroker) pluginFilePath(pluginID, rel string) (string, error) {
	if b == nil || b.store == nil {
		return "", fmt.Errorf("plugin file store is not configured")
	}
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative")
	}
	cleaned := filepath.ToSlash(filepath.Clean(rel))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("path must not contain ..")
	}
	stateDir, err := b.store.StateDir(pluginID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(stateDir, filepath.FromSlash(cleaned))
	if !sameOrUnder(path, stateDir) {
		return "", fmt.Errorf("path escapes plugin state")
	}
	return path, nil
}

func capabilityForFacadeMethod(method string) (Capability, bool) {
	switch method {
	case "plugin.info", "log.emit", "metric.emit":
		return "", false
	case "plugin.kv.get", "plugin.kv.set":
		return CapabilityPluginKV, true
	case "plugin.files.read_text", "plugin.files.write_text":
		return CapabilityPluginFiles, true
	case "memory.safe_context.current":
		return CapabilityMemoryReadSafe, true
	case "memory.candidate.submit":
		return CapabilityMemoryCandidateSubmit, true
	case "memory.forget.request":
		return CapabilityMemoryForgetRequest, true
	case "work.decision.observe":
		return CapabilityWorkObserve, true
	case "work.dispatch.annotate":
		return CapabilityWorkDispatchAnnotate, true
	case "approval.observe":
		return CapabilityApprovalObserve, true
	case "agent_affect.current":
		return CapabilityAgentAffectRead, true
	case "web.search", "web.fetch":
		return CapabilityNetworkWeb, true
	case "provider.generate":
		return CapabilityProviderGenerate, true
	default:
		return "", true
	}
}

func grantAllows(raw string, manifestTier AccessTier, capability Capability) error {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	var grant struct {
		Tier         AccessTier   `json:"tier"`
		Capabilities []Capability `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(raw), &grant); err != nil {
		return fmt.Errorf("invalid user grant: %w", err)
	}
	if grant.Tier != "" && accessTierRank(grant.Tier) < accessTierRank(manifestTier) {
		return fmt.Errorf("%w: grant tier %s is lower than manifest tier %s", ErrCapabilityDenied, grant.Tier, manifestTier)
	}
	if len(grant.Capabilities) == 0 {
		return nil
	}
	for _, allowed := range grant.Capabilities {
		if allowed == capability {
			return nil
		}
	}
	return fmt.Errorf("%w: user grant lacks %s", ErrCapabilityDenied, capability)
}

func accessTierRank(tier AccessTier) int {
	switch tier {
	case AccessTierRuntimeSafe:
		return 1
	case AccessTierUserContext:
		return 2
	case AccessTierWorkspace:
		return 3
	case AccessTierTrusted:
		return 4
	default:
		return 0
	}
}

func (b *FacadeBroker) recordAccess(ctx context.Context, event storage.PluginAccessEvent) error {
	if b == nil || b.storage == nil {
		return nil
	}
	return b.storage.RecordPluginAccessEvent(ctx, event)
}

func decodeFacadeParams(params json.RawMessage, target any) error {
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(strings.NewReader(string(params)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid facade params: %w", err)
	}
	return nil
}

func marshalRaw(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	return json.RawMessage(data), err
}

func summarizeFacadeRequest(method string, params json.RawMessage) string {
	if len(params) == 0 {
		return method
	}
	return method + " bytes=" + fmt.Sprint(len(params))
}

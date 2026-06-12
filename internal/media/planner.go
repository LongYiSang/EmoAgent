package media

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/longyisang/emoagent/internal/llm"
)

const (
	PolicyReject         = "reject"
	PolicyOptimisticSend = "optimistic_send"
	PolicyStripMedia     = "strip_media"
	PolicyForceSend      = "force_send"

	TransportDataURL      = "data_url"
	TransportBase64       = "base64"
	TransportRemoteURL    = "remote_url"
	TransportProviderFile = "provider_file"
	TransportPlaceholder  = "placeholder"

	DeliveryScopeCurrentTurn          = "current_turn"
	DeliveryScopeReactivatedReference = "reactivated_reference"
	DeliveryScopeHistoryPlaceholder   = "history_placeholder"

	DeliveryStatusPrepared = "prepared"
	DeliveryStatusSent     = "sent"
	DeliveryStatusFailed   = "failed"
	DeliveryStatusOmitted  = "omitted"
)

type CapabilityResolver interface {
	Resolve(ctx context.Context, providerID, modelID string) (*llm.ModelCapabilities, error)
}

type Store interface {
	Open(ctx context.Context, mediaAssetID string) (io.ReadCloser, *MediaAsset, error)
}

type MediaPolicy struct {
	Enabled                bool
	UnknownModelPolicy     string
	UnsupportedModelPolicy string
	PreferredTransports    []string
	MaxRequestBytes        int64
}

type PrepareRequest struct {
	ProviderID          string
	ModelID             string
	Messages            []llm.Message
	CurrentTurnID       string
	ReactivatedMediaIDs map[string]bool
	Policy              MediaPolicy
}

type PrepareResult struct {
	Messages   []llm.Message
	Deliveries []DeliveryRecord
}

type DeliveryRecord struct {
	MessageID     string
	PartID        string
	MediaAssetID  string
	ProviderID    string
	ModelID       string
	TurnID        string
	DeliveryScope string
	Transport     string
	Status        string
	ByteSizeSent  int64
	ErrorMessage  string
}

type Planner struct {
	store    Store
	resolver CapabilityResolver
}

func NewPlanner(store Store, resolver CapabilityResolver) *Planner {
	return &Planner{store: store, resolver: resolver}
}

func DefaultPolicy() MediaPolicy {
	return MediaPolicy{
		Enabled:                true,
		UnknownModelPolicy:     PolicyReject,
		UnsupportedModelPolicy: PolicyReject,
		PreferredTransports:    []string{TransportDataURL, TransportBase64},
	}
}

func (p *Planner) Prepare(ctx context.Context, req PrepareRequest) (*PrepareResult, error) {
	policy := normalizePolicy(req.Policy)
	rendered := llm.RenderMessages(req.Messages, llm.RenderForCurrentLLMTurn, llm.RenderPolicy{
		CurrentTurnID:       req.CurrentTurnID,
		ReactivatedMediaIDs: req.ReactivatedMediaIDs,
	})
	if !messagesContainMedia(rendered) {
		return &PrepareResult{Messages: rendered}, nil
	}
	if !policy.Enabled {
		return nil, fmt.Errorf("media input is disabled")
	}
	caps, err := p.resolve(ctx, req.ProviderID, req.ModelID)
	if err != nil {
		return nil, err
	}
	unknown := caps == nil || strings.TrimSpace(caps.CapabilitySource) == "" || caps.CapabilitySource == string(llm.CapabilitySourceUnknown)
	if unknown {
		switch policy.UnknownModelPolicy {
		case PolicyOptimisticSend, PolicyForceSend:
		case PolicyStripMedia:
			return &PrepareResult{
				Messages:   llm.RenderMessages(req.Messages, llm.RenderForHistory, llm.RenderPolicy{}),
				Deliveries: omittedDeliveries(req),
			}, nil
		default:
			return nil, fmt.Errorf("model %s/%s has unknown media capability", req.ProviderID, req.ModelID)
		}
	} else if !supportsImage(caps) {
		switch policy.UnsupportedModelPolicy {
		case PolicyForceSend:
		case PolicyStripMedia:
			return &PrepareResult{
				Messages:   llm.RenderMessages(req.Messages, llm.RenderForHistory, llm.RenderPolicy{}),
				Deliveries: omittedDeliveries(req),
			}, nil
		default:
			return nil, fmt.Errorf("model %s/%s does not support image input", req.ProviderID, req.ModelID)
		}
	}
	preferred := firstTransport(policy.PreferredTransports, caps)
	if preferred == "" {
		preferred = TransportDataURL
	}
	prepared := make([]llm.Message, len(rendered))
	deliveries := omittedHistoricalDeliveries(req)
	for i, msg := range rendered {
		prepared[i] = msg
		if len(msg.ContentBlocks) == 0 {
			continue
		}
		blocks := make([]llm.ContentBlock, len(msg.ContentBlocks))
		for j, block := range msg.ContentBlocks {
			blocks[j] = block
			if block.Media == nil {
				continue
			}
			if p.store == nil {
				return nil, fmt.Errorf("media store is required")
			}
			rc, asset, err := p.store.Open(ctx, block.Media.MediaAssetID)
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(rc)
			closeErr := rc.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			media := *block.Media
			media.Data = data
			media.MimeType = firstNonEmpty(media.MimeType, asset.MimeType)
			media.Kind = firstNonEmpty(media.Kind, asset.Kind)
			media.Transport = preferred
			blocks[j].Media = &media
			deliveries = append(deliveries, DeliveryRecord{
				MessageID:     msg.ID,
				PartID:        block.ID,
				MediaAssetID:  media.MediaAssetID,
				ProviderID:    req.ProviderID,
				ModelID:       req.ModelID,
				TurnID:        req.CurrentTurnID,
				DeliveryScope: sentDeliveryScope(msg, media.MediaAssetID, req),
				Transport:     preferred,
				Status:        DeliveryStatusPrepared,
				ByteSizeSent:  int64(len(data)),
			})
		}
		prepared[i].ContentBlocks = blocks
	}
	return &PrepareResult{Messages: prepared, Deliveries: deliveries}, nil
}

func (p *Planner) resolve(ctx context.Context, providerID, modelID string) (*llm.ModelCapabilities, error) {
	if p.resolver == nil {
		return nil, nil
	}
	return p.resolver.Resolve(ctx, providerID, modelID)
}

func normalizePolicy(policy MediaPolicy) MediaPolicy {
	defaults := DefaultPolicy()
	if policy.UnknownModelPolicy == "" {
		policy.UnknownModelPolicy = defaults.UnknownModelPolicy
	}
	if policy.UnsupportedModelPolicy == "" {
		policy.UnsupportedModelPolicy = defaults.UnsupportedModelPolicy
	}
	if len(policy.PreferredTransports) == 0 {
		policy.PreferredTransports = defaults.PreferredTransports
	}
	if !policy.Enabled && policy.UnknownModelPolicy == "" && policy.UnsupportedModelPolicy == "" {
		policy.Enabled = defaults.Enabled
	}
	return policy
}

func messagesContainMedia(messages []llm.Message) bool {
	for _, msg := range messages {
		for _, block := range msg.ContentBlocks {
			if block.Media != nil {
				return true
			}
		}
	}
	return false
}

func supportsImage(caps *llm.ModelCapabilities) bool {
	if caps == nil {
		return false
	}
	for _, modality := range caps.InputModalities {
		if modality == "image" {
			return len(caps.ImageTransports) > 0
		}
	}
	return false
}

func firstTransport(preferred []string, caps *llm.ModelCapabilities) string {
	if caps == nil || len(caps.ImageTransports) == 0 {
		if len(preferred) > 0 {
			return preferred[0]
		}
		return ""
	}
	allowed := map[string]bool{}
	for _, transport := range caps.ImageTransports {
		allowed[transport] = true
	}
	for _, transport := range preferred {
		if allowed[transport] {
			return transport
		}
	}
	return caps.ImageTransports[0]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func omittedHistoricalDeliveries(req PrepareRequest) []DeliveryRecord {
	deliveries := make([]DeliveryRecord, 0)
	for _, msg := range req.Messages {
		if shouldKeepMedia(msg, req) {
			continue
		}
		deliveries = append(deliveries, omittedMessageDeliveries(msg, req)...)
	}
	return deliveries
}

func omittedDeliveries(req PrepareRequest) []DeliveryRecord {
	deliveries := make([]DeliveryRecord, 0)
	for _, msg := range req.Messages {
		deliveries = append(deliveries, omittedMessageDeliveries(msg, req)...)
	}
	return deliveries
}

func omittedMessageDeliveries(msg llm.Message, req PrepareRequest) []DeliveryRecord {
	if len(msg.ContentBlocks) == 0 {
		return nil
	}
	scope := DeliveryScopeHistoryPlaceholder
	if msg.TurnID != "" && msg.TurnID == req.CurrentTurnID {
		scope = DeliveryScopeCurrentTurn
	} else {
		for _, block := range msg.ContentBlocks {
			if block.Media != nil && req.ReactivatedMediaIDs[block.Media.MediaAssetID] {
				scope = DeliveryScopeReactivatedReference
				break
			}
		}
	}
	deliveries := make([]DeliveryRecord, 0)
	for _, block := range msg.ContentBlocks {
		if block.Media == nil {
			continue
		}
		deliveries = append(deliveries, DeliveryRecord{
			MessageID:     msg.ID,
			PartID:        block.ID,
			MediaAssetID:  block.Media.MediaAssetID,
			ProviderID:    req.ProviderID,
			ModelID:       req.ModelID,
			TurnID:        req.CurrentTurnID,
			DeliveryScope: scope,
			Transport:     TransportPlaceholder,
			Status:        DeliveryStatusOmitted,
		})
	}
	return deliveries
}

func shouldKeepMedia(msg llm.Message, req PrepareRequest) bool {
	if strings.TrimSpace(req.CurrentTurnID) != "" && msg.TurnID == req.CurrentTurnID {
		return true
	}
	if len(req.ReactivatedMediaIDs) == 0 {
		return false
	}
	for _, block := range msg.ContentBlocks {
		if block.Media != nil && req.ReactivatedMediaIDs[block.Media.MediaAssetID] {
			return true
		}
	}
	return false
}

func sentDeliveryScope(msg llm.Message, mediaAssetID string, req PrepareRequest) string {
	if strings.TrimSpace(req.CurrentTurnID) != "" && msg.TurnID == req.CurrentTurnID {
		return DeliveryScopeCurrentTurn
	}
	if req.ReactivatedMediaIDs[mediaAssetID] {
		return DeliveryScopeReactivatedReference
	}
	return DeliveryScopeHistoryPlaceholder
}

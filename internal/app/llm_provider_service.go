package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

type LLMProviderService struct {
	infra *Infra
}

func (s *LLMProviderService) List() ([]config.LLMProvider, error) {
	records, err := s.infra.DB.ListLLMProviders()
	if err != nil {
		return nil, err
	}
	providers := make([]config.LLMProvider, 0, len(records))
	for _, record := range records {
		providers = append(providers, record.LLMProvider)
	}
	return providers, nil
}

func (s *LLMProviderService) Get(id string) (*config.LLMProvider, error) {
	record, err := s.infra.DB.GetLLMProvider(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrLLMProviderNotFound
	}
	provider := record.LLMProvider
	return &provider, nil
}

func (s *LLMProviderService) Create(provider config.LLMProvider) error {
	var err error
	provider, err = provider.WithPresetDefaults()
	if err != nil {
		return err
	}
	if err := provider.Validate(); err != nil {
		return err
	}
	existing, err := s.infra.DB.GetLLMProvider(context.Background(), provider.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrLLMProviderExists
	}
	return s.infra.DB.UpsertLLMProvider(provider)
}

func (s *LLMProviderService) Update(id string, provider config.LLMProvider) error {
	provider.ID = id
	var err error
	provider, err = provider.WithPresetDefaults()
	if err != nil {
		return err
	}
	if err := provider.Validate(); err != nil {
		return err
	}
	existing, err := s.infra.DB.GetLLMProvider(context.Background(), id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrLLMProviderNotFound
	}
	return s.infra.DB.UpsertLLMProvider(provider)
}

func (s *LLMProviderService) Delete(id string) error {
	err := s.infra.DB.DeleteLLMProvider(id)
	if errors.Is(err, storage.ErrProviderInUse) {
		return ErrLLMProviderInUse
	}
	return err
}

func (s *LLMProviderService) RefreshModels(id string) ([]llm.ModelInfo, error) {
	provider, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if provider.ModelDiscovery == "manual" || provider.ModelDiscovery == "" {
		return []llm.ModelInfo{}, nil
	}
	models, err := llm.DiscoverModels(context.Background(), llm.ProviderConfig{
		ID:             provider.ID,
		PresetID:       provider.PresetID,
		Protocol:       provider.Protocol,
		BaseURL:        provider.BaseURL,
		APIKeyEnv:      provider.APIKeyEnv,
		ModelDiscovery: provider.ModelDiscovery,
	})
	if err != nil {
		return nil, err
	}
	refreshedAt := s.nowText()
	models, err = s.persistModelCapabilities(provider, models, refreshedAt)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	if err := s.infra.DB.UpdateProviderModelsCache(id, string(payload), refreshedAt); err != nil {
		return nil, err
	}
	return models, nil
}

func (s *LLMProviderService) Models(id string) ([]llm.ModelInfo, error) {
	record, err := s.infra.DB.GetLLMProvider(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrLLMProviderNotFound
	}
	var models []llm.ModelInfo
	if strings.TrimSpace(record.ModelsCacheJSON) == "" {
		return []llm.ModelInfo{}, nil
	}
	if err := json.Unmarshal([]byte(record.ModelsCacheJSON), &models); err != nil {
		return nil, err
	}
	caps, err := s.infra.DB.ListModelCapabilities(context.Background(), id)
	if err != nil {
		return nil, err
	}
	for i := range models {
		if capRecord, ok := caps[models[i].ID]; ok {
			cap := modelCapabilityFromRecord(capRecord)
			models[i].Capabilities = &cap
		}
	}
	return models, nil
}

func (s *LLMProviderService) Resolve(ctx context.Context, providerID, modelID string) (*llm.ModelCapabilities, error) {
	record, err := s.infra.DB.GetModelCapability(ctx, providerID, modelID)
	if err != nil || record == nil {
		return nil, err
	}
	cap := modelCapabilityFromRecord(*record)
	return &cap, nil
}

func (s *LLMProviderService) persistModelCapabilities(provider *config.LLMProvider, models []llm.ModelInfo, refreshedAt string) ([]llm.ModelInfo, error) {
	existing, err := s.infra.DB.ListModelCapabilities(context.Background(), provider.ID)
	if err != nil {
		return nil, err
	}
	manual := map[string]llm.ModelCapabilities{}
	for modelID, record := range existing {
		if record.CapabilitySource == string(llm.CapabilitySourceManualOverride) {
			manual[modelID] = modelCapabilityFromRecord(record)
		}
	}
	caps := llm.EnrichModelCapabilities(provider.ID, provider.PresetID, models, refreshedAt, manual)
	byModel := map[string]llm.ModelCapabilities{}
	for _, cap := range caps {
		byModel[cap.ModelID] = cap
		if err := s.infra.DB.UpsertModelCapability(context.Background(), modelCapabilityToRecord(cap)); err != nil {
			return nil, err
		}
	}
	for i := range models {
		if cap, ok := byModel[models[i].ID]; ok {
			capCopy := cap
			models[i].Capabilities = &capCopy
		}
	}
	return models, nil
}

func modelCapabilityFromRecord(record storage.ModelCapabilityRecord) llm.ModelCapabilities {
	return llm.ModelCapabilities{
		ProviderID:              record.ProviderID,
		ModelID:                 record.ModelID,
		InputModalities:         append([]string(nil), record.InputModalities...),
		OutputModalities:        append([]string(nil), record.OutputModalities...),
		ImageTransports:         append([]string(nil), record.ImageTransports...),
		ImageFormats:            append([]string(nil), record.ImageFormats...),
		MaxImagesPerRequest:     record.MaxImagesPerRequest,
		MaxImageBytes:           record.MaxImageBytes,
		MaxRequestBytes:         record.MaxRequestBytes,
		MaxLongEdgePixels:       record.MaxLongEdgePixels,
		SupportsVisionTools:     record.SupportsVisionTools,
		SupportsVisionStreaming: record.SupportsVisionStreaming,
		SupportsVisionJSONMode:  record.SupportsVisionJSONMode,
		ParamPolicyJSON:         record.ParamPolicyJSON,
		CapabilitySource:        record.CapabilitySource,
		Confidence:              record.Confidence,
		LastRefreshedAt:         record.LastRefreshedAt,
		LastVerifiedAt:          record.LastVerifiedAt,
		RawProviderJSON:         record.RawProviderJSON,
	}
}

func modelCapabilityToRecord(cap llm.ModelCapabilities) storage.ModelCapabilityRecord {
	return storage.ModelCapabilityRecord{
		ProviderID:              cap.ProviderID,
		ModelID:                 cap.ModelID,
		InputModalities:         append([]string(nil), cap.InputModalities...),
		OutputModalities:        append([]string(nil), cap.OutputModalities...),
		ImageTransports:         append([]string(nil), cap.ImageTransports...),
		ImageFormats:            append([]string(nil), cap.ImageFormats...),
		MaxImagesPerRequest:     cap.MaxImagesPerRequest,
		MaxImageBytes:           cap.MaxImageBytes,
		MaxRequestBytes:         cap.MaxRequestBytes,
		MaxLongEdgePixels:       cap.MaxLongEdgePixels,
		SupportsVisionTools:     cap.SupportsVisionTools,
		SupportsVisionStreaming: cap.SupportsVisionStreaming,
		SupportsVisionJSONMode:  cap.SupportsVisionJSONMode,
		ParamPolicyJSON:         cap.ParamPolicyJSON,
		CapabilitySource:        cap.CapabilitySource,
		Confidence:              cap.Confidence,
		LastRefreshedAt:         cap.LastRefreshedAt,
		LastVerifiedAt:          cap.LastVerifiedAt,
		RawProviderJSON:         cap.RawProviderJSON,
	}
}

func (s *LLMProviderService) EnvStatus(id string) (configcenter.ProviderEnvStatus, error) {
	provider, err := s.Get(id)
	if err != nil {
		return configcenter.ProviderEnvStatus{}, err
	}
	_, present := os.LookupEnv(strings.TrimSpace(provider.APIKeyEnv))
	return configcenter.ProviderEnvStatus{
		APIKeyEnv: strings.TrimSpace(provider.APIKeyEnv),
		Present:   strings.TrimSpace(provider.APIKeyEnv) != "" && present,
	}, nil
}

func (s *LLMProviderService) nowText() string {
	timezone := "Asia/Shanghai"
	if s != nil && s.infra != nil && s.infra.Config != nil && strings.TrimSpace(s.infra.Config.Time.Timezone) != "" {
		timezone = s.infra.Config.Time.Timezone
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return time.Now().In(loc).Format(time.RFC3339Nano)
}

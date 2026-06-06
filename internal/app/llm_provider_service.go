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
		ID:        provider.ID,
		PresetID:  provider.PresetID,
		Protocol:  provider.Protocol,
		BaseURL:   provider.BaseURL,
		APIKeyEnv: provider.APIKeyEnv,
	})
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	if err := s.infra.DB.UpdateProviderModelsCache(id, string(payload), s.nowText()); err != nil {
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
	return models, nil
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

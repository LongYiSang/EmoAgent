package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

const personaWatchInterval = 5 * time.Second

type PersonaService struct {
	infra    *Infra
	mu       sync.RWMutex
	personas map[string]*config.Persona
}

func (s *PersonaService) LoadAndSync() error {
	personas, err := config.LoadAllPersonas(s.infra.Config.Personas.Dir)
	if err != nil {
		s.infra.Logger.Warn("load personas failed", "error", err)
		personas = make(map[string]*config.Persona)
	}
	s.SetAll(personas)
	s.infra.Logger.Info("personas loaded", "count", len(personas))
	return s.SyncToDB(personas, "sync persona to db failed")
}

func (s *PersonaService) Watch(ctx context.Context) {
	go config.WatchPersonas(ctx, s.infra.Config.Personas.Dir, personaWatchInterval, func(updated map[string]*config.Persona) {
		s.SetAll(updated)
		if err := s.SyncToDB(updated, "sync updated persona failed"); err != nil {
			s.infra.Logger.Warn("sync updated personas failed", "error", err)
		}
		s.infra.Logger.Info("personas reloaded", "count", len(updated))
	})
}

func (s *PersonaService) SyncToDB(personas map[string]*config.Persona, logMessage string) error {
	for key, p := range personas {
		if err := s.infra.DB.UpsertPersona(key, p.Name, p.Description, p.SystemPrompt, p.Tone, p.Quirks, p.Greeting, p.WorkProgressPhrases); err != nil {
			s.infra.Logger.Warn(logMessage, "key", key, "name", p.Name, "error", err)
		}
	}
	return nil
}

func (s *PersonaService) SetAll(personas map[string]*config.Persona) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.personas = clonePersonaMap(personas)
}

func (s *PersonaService) Get(name string) (*config.Persona, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.personas[name]
	if !ok {
		return nil, false
	}
	return clonePersona(p), true
}

func (s *PersonaService) Exists(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.personas[name]
	return ok
}

func (s *PersonaService) List() map[string]*config.Persona {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePersonaMap(s.personas)
}

func (s *PersonaService) Create(key string, p *config.Persona) error {
	if p == nil {
		return fmt.Errorf("persona is required")
	}
	if key == "" {
		return fmt.Errorf("persona key is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.personas[key]; exists {
		return ErrPersonaExists
	}

	next := clonePersona(p)
	if next.Name == "" {
		next.Name = key
	}
	if err := config.SavePersona(s.infra.Config.Personas.Dir, key, next); err != nil {
		return fmt.Errorf("save persona file: %w", err)
	}
	if err := s.infra.DB.UpsertPersona(key, next.Name, next.Description, next.SystemPrompt, next.Tone, next.Quirks, next.Greeting, next.WorkProgressPhrases); err != nil {
		return fmt.Errorf("upsert persona: %w", err)
	}
	s.personas[key] = next
	return nil
}

func (s *PersonaService) Update(key string, p *config.Persona) error {
	if p == nil {
		return fmt.Errorf("persona is required")
	}
	if key == "" {
		return fmt.Errorf("persona key is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.personas[key]; !exists {
		return ErrPersonaNotFound
	}

	next := clonePersona(p)
	if next.Name == "" {
		next.Name = key
	}
	if err := config.SavePersona(s.infra.Config.Personas.Dir, key, next); err != nil {
		return fmt.Errorf("save persona file: %w", err)
	}
	if err := s.infra.DB.UpsertPersona(key, next.Name, next.Description, next.SystemPrompt, next.Tone, next.Quirks, next.Greeting, next.WorkProgressPhrases); err != nil {
		return fmt.Errorf("upsert persona: %w", err)
	}
	s.personas[key] = next
	return nil
}

func (s *PersonaService) GetProgressPhrases(key string) (map[string][]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	persona, exists := s.personas[key]
	if !exists || persona == nil {
		return nil, ErrPersonaNotFound
	}
	return cloneProgressPhrases(persona.WorkProgressPhrases), nil
}

func (s *PersonaService) UpdateProgressPhrases(key string, phrases map[string][]string) error {
	if key == "" {
		return fmt.Errorf("persona key is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.personas[key]
	if !exists || current == nil {
		return ErrPersonaNotFound
	}

	next := clonePersona(current)
	next.WorkProgressPhrases = cloneProgressPhrases(phrases)
	if err := config.SavePersona(s.infra.Config.Personas.Dir, key, next); err != nil {
		return fmt.Errorf("save persona file: %w", err)
	}
	if err := s.infra.DB.UpsertPersona(key, next.Name, next.Description, next.SystemPrompt, next.Tone, next.Quirks, next.Greeting, next.WorkProgressPhrases); err != nil {
		return fmt.Errorf("upsert persona: %w", err)
	}
	s.personas[key] = next
	return nil
}

func (s *PersonaService) Delete(key string, activePersona string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if key == activePersona {
		return ErrCannotDeleteDefault
	}
	if _, exists := s.personas[key]; !exists {
		return ErrPersonaNotFound
	}
	if err := config.DeletePersonaFile(s.infra.Config.Personas.Dir, key); err != nil {
		return fmt.Errorf("delete persona file: %w", err)
	}
	if err := s.infra.DB.DeletePersona(context.Background(), key); err != nil {
		return fmt.Errorf("delete persona from db: %w", err)
	}
	delete(s.personas, key)
	return nil
}

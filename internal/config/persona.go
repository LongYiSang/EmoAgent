package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Persona struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Tone         string   `yaml:"tone"`
	Quirks       []string `yaml:"quirks"`
	Greeting     string   `yaml:"greeting"`
}

// LoadPersona reads a single persona YAML file.
func LoadPersona(path string) (*Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read persona %s: %w", path, err)
	}

	var p Persona
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse persona %s: %w", path, err)
	}

	if p.Name == "" {
		base := filepath.Base(path)
		p.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}

	return &p, nil
}

// LoadAllPersonas loads all .yaml/.yml files from a directory.
func LoadAllPersonas(dir string) (map[string]*Persona, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*Persona), nil
		}
		return nil, fmt.Errorf("read personas dir: %w", err)
	}

	personas := make(map[string]*Persona)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		p, err := LoadPersona(filepath.Join(dir, e.Name()))
		if err != nil {
			slog.Warn("skip persona file", "file", e.Name(), "error", err)
			continue
		}
		key := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		personas[key] = p
	}

	return personas, nil
}

// WatchPersonas polls the persona directory for changes and calls onChange when files are modified.
// It blocks until ctx is cancelled.
func WatchPersonas(ctx context.Context, dir string, interval time.Duration, onChange func(map[string]*Persona)) {
	modTimes := make(map[string]time.Time)

	// snapshot current mod times
	snapshot := func() map[string]time.Time {
		m := make(map[string]time.Time)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return m
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			m[e.Name()] = info.ModTime()
		}
		return m
	}

	modTimes = snapshot()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := snapshot()
			changed := len(current) != len(modTimes)
			if !changed {
				for name, t := range current {
					if prev, ok := modTimes[name]; !ok || !prev.Equal(t) {
						changed = true
						break
					}
				}
			}
			if changed {
				slog.Info("persona files changed, reloading", "dir", dir)
				personas, err := LoadAllPersonas(dir)
				if err != nil {
					slog.Error("reload personas failed", "error", err)
				} else {
					onChange(personas)
				}
				modTimes = current
			}
		}
	}
}

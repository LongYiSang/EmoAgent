package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPersona(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(`
name: "TestBot"
description: "A test persona"
system_prompt: "You are a test bot."
tone: "neutral"
quirks:
  - "says beep boop"
greeting: "Hello, I am TestBot."
`), 0o644)

	p, err := LoadPersona(path)
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}
	if p.Name != "TestBot" {
		t.Errorf("name = %q, want TestBot", p.Name)
	}
	if len(p.Quirks) != 1 {
		t.Errorf("quirks count = %d, want 1", len(p.Quirks))
	}
}

func TestLoadPersonaInfersName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mybot.yaml")
	os.WriteFile(path, []byte(`
description: "No name field"
system_prompt: "You are mybot."
`), 0o644)

	p, err := LoadPersona(path)
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}
	if p.Name != "mybot" {
		t.Errorf("inferred name = %q, want mybot", p.Name)
	}
}

func TestLoadAllPersonas(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("name: alpha\nsystem_prompt: hi"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.yml"), []byte("name: beta\nsystem_prompt: hi"), 0o644)
	os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("not yaml"), 0o644)

	personas, err := LoadAllPersonas(dir)
	if err != nil {
		t.Fatalf("LoadAllPersonas: %v", err)
	}
	if len(personas) != 2 {
		t.Errorf("loaded %d personas, want 2", len(personas))
	}
}

func TestLoadAllPersonasUsesFilenameAsKey(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "default.yaml"), []byte("name: Emo\nsystem_prompt: hi"), 0o644)

	personas, err := LoadAllPersonas(dir)
	if err != nil {
		t.Fatalf("LoadAllPersonas: %v", err)
	}

	persona, ok := personas["default"]
	if !ok {
		t.Fatalf("expected personas[\"default\"] to exist, got keys: %#v", personas)
	}
	if persona.Name != "Emo" {
		t.Fatalf("persona.Name = %q, want Emo", persona.Name)
	}
}

func TestLoadAllPersonasEmptyDir(t *testing.T) {
	personas, err := LoadAllPersonas("/nonexistent/dir")
	if err != nil {
		t.Fatalf("expected empty map for missing dir, got error: %v", err)
	}
	if len(personas) != 0 {
		t.Errorf("expected empty map, got %d", len(personas))
	}
}

func TestSavePersonaAndDeletePersonaFile(t *testing.T) {
	dir := t.TempDir()
	persona := &Persona{
		Name:         "Emo",
		Description:  "Warm companion",
		SystemPrompt: "You are Emo.",
		Tone:         "warm",
		Quirks:       []string{"gentle"},
		Greeting:     "Hello",
		WorkProgressPhrases: map[string][]string{
			"read_file": {"看看文件"},
			"_default":  {"处理中"},
		},
	}

	if err := SavePersona(dir, "default", persona); err != nil {
		t.Fatalf("SavePersona: %v", err)
	}

	loaded, err := LoadPersona(filepath.Join(dir, "default.yaml"))
	if err != nil {
		t.Fatalf("LoadPersona(saved): %v", err)
	}
	if loaded.Name != "Emo" {
		t.Fatalf("loaded.Name = %q, want Emo", loaded.Name)
	}
	if got := loaded.WorkProgressPhrases["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("loaded.WorkProgressPhrases = %#v, want read_file phrase", loaded.WorkProgressPhrases)
	}

	if err := DeletePersonaFile(dir, "default"); err != nil {
		t.Fatalf("DeletePersonaFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "default.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err = %v", err)
	}
}

func TestSavePersonaRejectsUnsafeKey(t *testing.T) {
	dir := t.TempDir()

	err := SavePersona(dir, filepath.Join("..", "escape"), &Persona{Name: "Escape"})
	if err == nil {
		t.Fatal("SavePersona with unsafe key returned nil error")
	}
}

func TestDeletePersonaFileRejectsUnsafeKey(t *testing.T) {
	dir := t.TempDir()

	err := DeletePersonaFile(dir, filepath.Join("..", "escape"))
	if err == nil {
		t.Fatal("DeletePersonaFile with unsafe key returned nil error")
	}
}

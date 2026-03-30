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

func TestLoadAllPersonasEmptyDir(t *testing.T) {
	personas, err := LoadAllPersonas("/nonexistent/dir")
	if err != nil {
		t.Fatalf("expected empty map for missing dir, got error: %v", err)
	}
	if len(personas) != 0 {
		t.Errorf("expected empty map, got %d", len(personas))
	}
}

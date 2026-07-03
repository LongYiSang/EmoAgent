package command

import (
	"strings"
	"testing"
)

func TestRegistryRejectsNameConflict(t *testing.T) {
	registry := NewRegistry()
	if err := registry.TryRegister(CommandDescriptor{
		ID:           "builtin.echo",
		Name:         "echo",
		ProviderKind: CommandProviderBuiltin,
	}); err != nil {
		t.Fatalf("register builtin echo: %v", err)
	}

	err := registry.TryRegister(CommandDescriptor{
		ID:           "plugin.example.echo",
		Name:         "echo",
		ProviderKind: CommandProviderPlugin,
		PluginID:     "example",
	})
	if err == nil {
		t.Fatal("plugin duplicate name error = nil, want conflict")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("error = %v, want already registered", err)
	}
}

func TestRegistryRejectsAliasConflict(t *testing.T) {
	registry := NewRegistry()
	if err := registry.TryRegister(CommandDescriptor{
		ID:           "builtin.echo",
		Name:         "echo",
		Aliases:      []string{"e"},
		ProviderKind: CommandProviderBuiltin,
	}); err != nil {
		t.Fatalf("register builtin echo: %v", err)
	}

	err := registry.TryRegister(CommandDescriptor{
		ID:           "plugin.example.echo2",
		Name:         "echo2",
		Aliases:      []string{"e"},
		ProviderKind: CommandProviderPlugin,
		PluginID:     "example",
	})
	if err == nil {
		t.Fatal("plugin alias conflict error = nil, want conflict")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("error = %v, want already registered", err)
	}
}

func TestRegistryRejectsPluginReservedRoot(t *testing.T) {
	registry := NewRegistry()

	err := registry.TryRegister(CommandDescriptor{
		ID:           "plugin.example.reset",
		Name:         "reset",
		ProviderKind: CommandProviderPlugin,
		PluginID:     "example",
	})
	if err == nil {
		t.Fatal("plugin reserved root error = nil, want error")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("error = %v, want reserved root error", err)
	}
}

func TestRegistryRejectsPluginReservedAlias(t *testing.T) {
	registry := NewRegistry()

	err := registry.TryRegister(CommandDescriptor{
		ID:           "plugin.example.weather",
		Name:         "weather",
		Aliases:      []string{"sid"},
		ProviderKind: CommandProviderPlugin,
		PluginID:     "example",
	})
	if err == nil {
		t.Fatal("plugin reserved alias error = nil, want error")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("error = %v, want reserved alias error", err)
	}
}

func TestRegistryUnregisterPluginRemovesRootsAndIDs(t *testing.T) {
	registry := NewRegistry()
	if err := registry.TryRegister(CommandDescriptor{
		ID:           "plugin.example.weather",
		Name:         "weather",
		Aliases:      []string{"forecast"},
		ProviderKind: CommandProviderPlugin,
		PluginID:     "example",
	}); err != nil {
		t.Fatalf("register plugin command: %v", err)
	}
	if err := registry.TryRegister(CommandDescriptor{
		ID:           "builtin.sid",
		Name:         "sid",
		ProviderKind: CommandProviderBuiltin,
	}); err != nil {
		t.Fatalf("register builtin command: %v", err)
	}

	removed := registry.UnregisterPlugin("example")

	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := registry.Get("plugin.example.weather"); ok {
		t.Fatal("plugin command id still registered")
	}
	if _, ok := registry.Lookup("weather"); ok {
		t.Fatal("plugin command root still registered")
	}
	if _, ok := registry.Lookup("forecast"); ok {
		t.Fatal("plugin command alias still registered")
	}
	if _, ok := registry.Lookup("sid"); !ok {
		t.Fatal("builtin command was removed")
	}
}

func TestReservedRootCommandNames(t *testing.T) {
	if !IsReservedRoot("reset") {
		t.Fatal("reset should be reserved")
	}
	if IsReservedRoot("weather") {
		t.Fatal("weather should not be reserved")
	}
}

package app

import "testing"

func TestKernelInitializesConversationAndCommandServices(t *testing.T) {
	a := newTestApp(nil, nil, nil)

	if a.kernel.Services.Conversation == nil {
		t.Fatal("Conversation service = nil")
	}
	if a.kernel.Services.Conversation.RunRegistry() == nil {
		t.Fatal("Conversation run registry = nil")
	}
	if a.kernel.Services.Commands == nil {
		t.Fatal("Command service = nil")
	}
	if _, ok := a.kernel.Services.Commands.Registry().Lookup("reset"); !ok {
		t.Fatal("builtin reset command was not registered")
	}
	if a.kernel.Services.Platforms == nil {
		t.Fatal("Platform service = nil")
	}
	if a.kernel.Services.Platforms.Gateway() == nil {
		t.Fatal("Platform gateway = nil")
	}
	if a.kernel.Services.Platforms.Manager() == nil {
		t.Fatal("Platform manager = nil")
	}
}

package chat

import (
	"net/http/httptest"
	"testing"
)

func TestResolveWSOriginAcceptsPlatformFields(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?origin_key=napcat:main:group:20002&source=napcat&adapter_instance_id=main&platform_id=qq&channel_type=group&external_conversation_id=20002&external_actor_id=10001&display_name=Alice", nil)

	origin, err := resolveWSOrigin(req)
	if err != nil {
		t.Fatalf("resolveWSOrigin: %v", err)
	}
	if origin.OriginKey != "napcat:main:group:20002" ||
		origin.SourceType != "napcat" ||
		origin.AdapterInstanceID != "main" ||
		origin.PlatformID != "qq" ||
		origin.ChannelType != "group" ||
		origin.ExternalConversationID != "20002" ||
		origin.ExternalActorID != "10001" ||
		origin.DisplayName != "Alice" {
		t.Fatalf("origin = %#v, want all platform fields", origin)
	}
}

func TestResolveWSOriginKeepsDefaultWebUI(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws", nil)

	origin, err := resolveWSOrigin(req)
	if err != nil {
		t.Fatalf("resolveWSOrigin: %v", err)
	}
	if origin.OriginKey != "webui:local:main" || origin.SourceType != "webui" || origin.ChannelType != "web" {
		t.Fatalf("origin = %#v, want webui defaults", origin)
	}
}

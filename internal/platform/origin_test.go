package platform

import (
	"strings"
	"testing"
)

func TestBuildOriginKeyScopes(t *testing.T) {
	tests := []struct {
		name string
		req  OriginBuildRequest
		want string
	}{
		{
			name: "private uses actor id",
			req: OriginBuildRequest{
				SourceType:             "napcat",
				AdapterInstanceID:      "main",
				ChannelType:            "private",
				ExternalConversationID: "ignored",
				ExternalActorID:        "10001",
				Scope:                  OriginScopePrivate,
			},
			want: "napcat:main:private:10001",
		},
		{
			name: "group shared",
			req: OriginBuildRequest{
				SourceType:             "napcat",
				AdapterInstanceID:      "main",
				ChannelType:            "group",
				ExternalConversationID: "20002",
				ExternalActorID:        "10001",
				Scope:                  OriginScopeGroupShared,
			},
			want: "napcat:main:group:20002",
		},
		{
			name: "group user unique",
			req: OriginBuildRequest{
				SourceType:             "napcat",
				AdapterInstanceID:      "main",
				ChannelType:            "group",
				ExternalConversationID: "20002",
				ExternalActorID:        "10001",
				Scope:                  OriginScopeGroupUserUnique,
			},
			want: "napcat:main:group_user:20002:10001",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildOriginKey(tt.req)
			if err != nil {
				t.Fatalf("BuildOriginKey: %v", err)
			}
			if got != tt.want {
				t.Fatalf("origin key = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildOriginKeySanitizesSegments(t *testing.T) {
	got, err := BuildOriginKey(OriginBuildRequest{
		SourceType:             " napcat ",
		AdapterInstanceID:      "main/机器人",
		ChannelType:            "private",
		ExternalConversationID: " user 10001 ",
		Scope:                  OriginScopePrivate,
	})
	if err != nil {
		t.Fatalf("BuildOriginKey: %v", err)
	}
	if got != "napcat:main:private:user_10001" {
		t.Fatalf("origin key = %q, want sanitized segments", got)
	}
}

func TestBuildOriginKeyLimitsSegmentLength(t *testing.T) {
	longID := strings.Repeat("a", maxOriginSegmentLength+20)
	got, err := BuildOriginKey(OriginBuildRequest{
		SourceType:             "napcat",
		AdapterInstanceID:      "main",
		ChannelType:            "private",
		ExternalConversationID: longID,
		Scope:                  OriginScopePrivate,
	})
	if err != nil {
		t.Fatalf("BuildOriginKey: %v", err)
	}
	segments := strings.Split(got, ":")
	if len(segments) != 4 {
		t.Fatalf("origin key = %q, want 4 segments", got)
	}
	if len(segments[3]) != maxOriginSegmentLength || !strings.Contains(segments[3], "_") {
		t.Fatalf("actor segment = %q, want capped segment with hash suffix", segments[3])
	}
}

func TestOriginFromInboundFillsConversationOrigin(t *testing.T) {
	origin, err := OriginFromInbound(InboundMessage{
		ExternalMessageID:      "msg-1",
		SourceType:             "napcat",
		AdapterInstanceID:      "main",
		PlatformID:             "qq",
		ChannelType:            "group",
		ExternalConversationID: "20002",
		ExternalActorID:        "10001",
		Actor: Actor{
			ID:          "10001",
			DisplayName: "Alice",
			Role:        ActorRoleAdmin,
		},
	}, OriginScopeGroupShared)
	if err != nil {
		t.Fatalf("OriginFromInbound: %v", err)
	}
	if origin.OriginKey != "napcat:main:group:20002" ||
		origin.SourceType != "napcat" ||
		origin.AdapterInstanceID != "main" ||
		origin.PlatformID != "qq" ||
		origin.ChannelType != "group" ||
		origin.ExternalConversationID != "20002" ||
		origin.ExternalActorID != "10001" ||
		origin.DisplayName != "Alice" {
		t.Fatalf("origin = %#v, want full inbound mapping", origin)
	}
}

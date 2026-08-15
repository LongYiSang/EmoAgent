package proactive

import (
	"context"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

type fakeTargetStore struct {
	candidates []storage.ProactiveTargetCandidate
}

func (s *fakeTargetStore) ListProactiveTargetCandidates(context.Context, string) ([]storage.ProactiveTargetCandidate, error) {
	return s.candidates, nil
}

func platformCandidate(originKey, channelType string) storage.ProactiveTargetCandidate {
	return storage.ProactiveTargetCandidate{
		OriginKey:              originKey,
		SourceType:             "platform",
		AdapterInstanceID:      "onebot-1",
		ChannelType:            channelType,
		ExternalConversationID: "123",
		ExternalActorID:        "123",
		SessionID:              "session-" + originKey,
	}
}

func TestResolveTargetPicksMostRecentPrivateOrigin(t *testing.T) {
	// The store returns candidates newest-first.
	store := &fakeTargetStore{candidates: []storage.ProactiveTargetCandidate{
		platformCandidate("qq:recent", "private"),
		platformCandidate("qq:older", "private"),
	}}

	target, ok, err := ResolveTarget(context.Background(), store, config.DefaultProactiveConfig(), "default")
	if err != nil || !ok {
		t.Fatalf("ResolveTarget ok=%v err=%v", ok, err)
	}
	if target.Origin.OriginKey != "qq:recent" {
		t.Fatalf("origin = %q, want qq:recent", target.Origin.OriginKey)
	}
	if target.AdapterInstanceID != "onebot-1" || target.SessionID != "session-qq:recent" {
		t.Fatalf("target = %#v", target)
	}
}

// A bot speaking up unprompted in a group chat is a different order of social
// risk from a private message. It must never happen by default.
func TestResolveTargetSkipsGroupChannelsByDefault(t *testing.T) {
	store := &fakeTargetStore{candidates: []storage.ProactiveTargetCandidate{
		platformCandidate("qq:group", "group"),
	}}

	_, ok, err := ResolveTarget(context.Background(), store, config.DefaultProactiveConfig(), "default")
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if ok {
		t.Fatal("resolved a group channel with allow_group_channels=false")
	}
}

func TestResolveTargetPrefersPrivateOverGroupWhenBothPresent(t *testing.T) {
	store := &fakeTargetStore{candidates: []storage.ProactiveTargetCandidate{
		platformCandidate("qq:group", "group"),
		platformCandidate("qq:private", "private"),
	}}

	target, ok, _ := ResolveTarget(context.Background(), store, config.DefaultProactiveConfig(), "default")
	if !ok || target.Origin.OriginKey != "qq:private" {
		t.Fatalf("target = %#v ok=%v, want the private origin", target, ok)
	}
}

func TestResolveTargetAllowsGroupWhenExplicitlyEnabled(t *testing.T) {
	cfg := config.DefaultProactiveConfig()
	cfg.Targets.AllowGroupChannels = true
	store := &fakeTargetStore{candidates: []storage.ProactiveTargetCandidate{
		platformCandidate("qq:group", "group"),
	}}

	target, ok, _ := ResolveTarget(context.Background(), store, cfg, "default")
	if !ok || target.Origin.OriginKey != "qq:group" {
		t.Fatalf("target = %#v ok=%v, want the group origin", target, ok)
	}
}

func TestResolveTargetHonoursAllowList(t *testing.T) {
	cfg := config.DefaultProactiveConfig()
	cfg.Targets.AllowOrigins = []string{"qq:allowed"}
	store := &fakeTargetStore{candidates: []storage.ProactiveTargetCandidate{
		platformCandidate("qq:blocked", "private"),
		platformCandidate("qq:allowed", "private"),
	}}

	target, ok, _ := ResolveTarget(context.Background(), store, cfg, "default")
	if !ok || target.Origin.OriginKey != "qq:allowed" {
		t.Fatalf("target = %#v ok=%v, want qq:allowed", target, ok)
	}
}

// WebUI origins are not a proactive delivery target in this phase: there is no
// way to push to a browser that may not even be open.
func TestResolveTargetSkipsNonPlatformOrigins(t *testing.T) {
	webui := platformCandidate("webui:local:main", "private")
	webui.SourceType = "webui"
	store := &fakeTargetStore{candidates: []storage.ProactiveTargetCandidate{webui}}

	_, ok, _ := ResolveTarget(context.Background(), store, config.DefaultProactiveConfig(), "default")
	if ok {
		t.Fatal("resolved a webui origin; only platform origins are deliverable in this phase")
	}
}

func TestResolveTargetReturnsFalseWhenNothingAvailable(t *testing.T) {
	store := &fakeTargetStore{}
	_, ok, err := ResolveTarget(context.Background(), store, config.DefaultProactiveConfig(), "default")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want no target", ok, err)
	}
}

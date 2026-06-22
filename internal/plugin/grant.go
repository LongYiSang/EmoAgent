package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type UserGrant struct {
	Tier         AccessTier   `json:"tier"`
	Capabilities []Capability `json:"capabilities"`
}

func ValidateUserGrantForManifest(raw string, manifest ManifestV2) (UserGrant, error) {
	grant, err := DecodeUserGrant(raw)
	if err != nil {
		return UserGrant{}, err
	}
	if grant.Tier != "" {
		if !KnownAccessTier(grant.Tier) {
			return UserGrant{}, fmt.Errorf("user grant tier %q is unsupported", grant.Tier)
		}
		if grant.Tier != manifest.Access.Tier {
			return UserGrant{}, fmt.Errorf("user grant tier %s does not match manifest tier %s", grant.Tier, manifest.Access.Tier)
		}
	}
	manifestCapabilities := map[Capability]struct{}{}
	for _, capability := range manifest.Access.Capabilities {
		manifestCapabilities[capability] = struct{}{}
	}
	for _, capability := range grant.Capabilities {
		if !KnownCapability(capability) {
			return UserGrant{}, fmt.Errorf("unknown capability %q in user grant", capability)
		}
		if _, ok := manifestCapabilities[capability]; !ok {
			return UserGrant{}, fmt.Errorf("user grant capability %s is not declared by manifest", capability)
		}
	}
	return grant, nil
}

func DecodeUserGrant(raw string) (UserGrant, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var grant UserGrant
	if err := decoder.Decode(&grant); err != nil {
		return UserGrant{}, fmt.Errorf("invalid user grant: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == io.EOF {
		return grant, nil
	} else if err != nil {
		return UserGrant{}, fmt.Errorf("invalid user grant: %w", err)
	}
	return UserGrant{}, fmt.Errorf("invalid user grant: trailing JSON")
}

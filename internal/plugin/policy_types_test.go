package plugin

import "testing"

func TestManagedLocalPolicyTypeValues(t *testing.T) {
	if TrustBlocked != "blocked" ||
		TrustDeveloper != "developer" ||
		TrustUserTrusted != "user_trusted" ||
		TrustVerifiedPublisher != "verified_publisher" ||
		TrustOfficial != "official" {
		t.Fatalf("trust constants changed")
	}
	if ExposureHidden != "hidden" || ExposureWork != "work" || ExposureEmotion != "emotion" {
		t.Fatalf("exposure constants changed")
	}
	if InvocationAuto != "auto" || InvocationAsk != "ask" || InvocationDeny != "deny" {
		t.Fatalf("invocation constants changed")
	}
}

func TestBuildPluginTrustReviewRequiresAcknowledgementForToolPolicyExpansion(t *testing.T) {
	previous := PluginTrustSubject{
		PluginID:                "com.example.tools",
		Version:                 "0.1.0",
		SignatureStatus:         SignatureStatusUnsignedDev,
		PublisherID:             "dev",
		DefaultToolExposure:     ExposureWork,
		DefaultInvocationPolicy: InvocationAsk,
	}
	target := PluginTrustSubject{
		PluginID:                "com.example.tools",
		Version:                 "0.2.0",
		PackageDigest:           "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestDigest:          "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SignatureStatus:         SignatureStatusUnsignedDev,
		PublisherID:             "dev",
		DefaultToolExposure:     ExposureEmotion,
		DefaultInvocationPolicy: InvocationAuto,
	}

	review := BuildPluginTrustReview(previous, target)
	if !review.Required {
		t.Fatalf("review.Required = false, want true")
	}
	wantReasons := []string{"tool_exposure_added:emotion", "tool_invocation_auto_enabled"}
	if !sameStringSlice(review.Reasons, wantReasons) {
		t.Fatalf("reasons = %#v, want %#v", review.Reasons, wantReasons)
	}
	if review.Acknowledgement == nil {
		t.Fatalf("acknowledgement is nil")
	}
	if review.Acknowledgement.DefaultToolExposure != ExposureEmotion || review.Acknowledgement.DefaultInvocationPolicy != InvocationAuto {
		t.Fatalf("acknowledgement tool policy = %#v", review.Acknowledgement)
	}

	forged := *review.Acknowledgement
	forged.DefaultInvocationPolicy = InvocationAsk
	if err := ValidatePluginTrustAcknowledgement(review, &forged); err == nil {
		t.Fatalf("ValidatePluginTrustAcknowledgement accepted stale tool policy acknowledgement")
	}
}

func TestValidatePluginTrustAcknowledgementBindsTargetUserGrantHash(t *testing.T) {
	subject := PluginTrustSubject{
		PluginID:                "com.example.tools",
		Version:                 "0.2.0",
		PackageDigest:           "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestDigest:          "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SignatureStatus:         SignatureStatusUnsignedDev,
		PublisherID:             "dev",
		DefaultToolExposure:     ExposureWork,
		DefaultInvocationPolicy: InvocationAsk,
	}
	ack := BuildPluginTrustAcknowledgement(subject, []string{"user_grant_capability_added:provider.generate"})
	ack.TargetUserGrantHash = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	review := PluginTrustReview{
		Required:        true,
		Reasons:         ack.Reasons,
		Acknowledgement: &ack,
	}

	forged := ack
	forged.TargetUserGrantHash = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if err := ValidatePluginTrustAcknowledgement(review, &forged); err == nil {
		t.Fatalf("ValidatePluginTrustAcknowledgement accepted mismatched target grant hash")
	}
	missing := ack
	missing.TargetUserGrantHash = ""
	if err := ValidatePluginTrustAcknowledgement(review, &missing); err == nil {
		t.Fatalf("ValidatePluginTrustAcknowledgement accepted missing target grant hash")
	}
}

func TestHashUserGrantCanonicalizesCapabilityOrder(t *testing.T) {
	left, err := HashUserGrant(UserGrant{
		Tier:         AccessTierRuntimeSafe,
		Capabilities: []Capability{CapabilityProviderGenerate, CapabilityTurnRead},
	})
	if err != nil {
		t.Fatalf("HashUserGrant left: %v", err)
	}
	right, err := HashUserGrant(UserGrant{
		Tier:         AccessTierRuntimeSafe,
		Capabilities: []Capability{CapabilityTurnRead, CapabilityProviderGenerate, CapabilityTurnRead},
	})
	if err != nil {
		t.Fatalf("HashUserGrant right: %v", err)
	}
	if left == "" || left != right {
		t.Fatalf("grant hash left=%q right=%q, want canonical match", left, right)
	}
}

func TestFingerprintPluginHostPolicyCapturesPolicyInputs(t *testing.T) {
	base, err := FingerprintPluginHostPolicy(PluginHostPolicyFingerprintSubject{
		FacadePolicy:            FacadeHostPolicy{AllowedCapabilities: []Capability{CapabilityProviderGenerate, CapabilityTurnRead}},
		AllowActiveHooks:        false,
		DefaultToolExposure:     ExposureWork,
		DefaultInvocationPolicy: InvocationAsk,
	})
	if err != nil {
		t.Fatalf("FingerprintPluginHostPolicy base: %v", err)
	}
	reordered, err := FingerprintPluginHostPolicy(PluginHostPolicyFingerprintSubject{
		FacadePolicy:            FacadeHostPolicy{AllowedCapabilities: []Capability{CapabilityTurnRead, CapabilityProviderGenerate}},
		AllowActiveHooks:        false,
		DefaultToolExposure:     ExposureWork,
		DefaultInvocationPolicy: InvocationAsk,
	})
	if err != nil {
		t.Fatalf("FingerprintPluginHostPolicy reordered: %v", err)
	}
	if base == "" || base != reordered {
		t.Fatalf("fingerprints base=%q reordered=%q, want order-insensitive match", base, reordered)
	}
	activeHooks, err := FingerprintPluginHostPolicy(PluginHostPolicyFingerprintSubject{
		FacadePolicy:            FacadeHostPolicy{AllowedCapabilities: []Capability{CapabilityProviderGenerate, CapabilityTurnRead}},
		AllowActiveHooks:        true,
		DefaultToolExposure:     ExposureWork,
		DefaultInvocationPolicy: InvocationAsk,
	})
	if err != nil {
		t.Fatalf("FingerprintPluginHostPolicy active hooks: %v", err)
	}
	if activeHooks == base {
		t.Fatalf("fingerprint did not change when active hook policy changed")
	}
	autoTools, err := FingerprintPluginHostPolicy(PluginHostPolicyFingerprintSubject{
		FacadePolicy:            FacadeHostPolicy{AllowedCapabilities: []Capability{CapabilityProviderGenerate, CapabilityTurnRead}},
		AllowActiveHooks:        false,
		DefaultToolExposure:     ExposureWork,
		DefaultInvocationPolicy: InvocationAuto,
	})
	if err != nil {
		t.Fatalf("FingerprintPluginHostPolicy auto tools: %v", err)
	}
	if autoTools == base {
		t.Fatalf("fingerprint did not change when tool invocation policy changed")
	}
}

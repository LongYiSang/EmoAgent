package execution

import (
	"context"
	"strings"
	"testing"
)

type fakeDomainGrantStore struct {
	grants []DomainGrant
}

func (s fakeDomainGrantStore) AllowedDomains(context.Context) ([]DomainGrant, error) {
	return s.grants, nil
}

func TestNetworkPolicyDefaultDenyWithoutGrant(t *testing.T) {
	policy := NetworkPolicy{DefaultMode: NetworkDeny}
	err := policy.Authorize(context.Background(), NetworkRequest{Domain: "example.com", Protocol: "https", Port: 443})
	if err == nil || !strings.Contains(err.Error(), "no domain grant") {
		t.Fatalf("Authorize error = %v, want no domain grant", err)
	}
}

func TestNetworkPolicyAllowsExactDomainGrant(t *testing.T) {
	policy := NetworkPolicy{
		DefaultMode: NetworkDeny,
		GrantStore: fakeDomainGrantStore{grants: []DomainGrant{{
			Domain:   "Example.COM.",
			Protocol: "https",
			Port:     443,
		}}},
	}
	if err := policy.Authorize(context.Background(), NetworkRequest{Domain: "example.com", Protocol: "HTTPS", Port: 443}); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := policy.Authorize(context.Background(), NetworkRequest{Domain: "example.com", Protocol: "http", Port: 80}); err == nil {
		t.Fatal("Authorize should reject protocol/port mismatch")
	}
}

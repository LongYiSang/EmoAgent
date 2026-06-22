package resource

import (
	"context"
	"testing"
	"time"
)

type compileGrantStore struct{}

func (compileGrantStore) Create(context.Context, GrantEnvelope) (GrantEnvelope, error) {
	return GrantEnvelope{}, nil
}

func (compileGrantStore) Get(context.Context, string) (GrantEnvelope, bool, error) {
	return GrantEnvelope{}, false, nil
}

func (compileGrantStore) List(context.Context, GrantListFilter) ([]GrantEnvelope, error) {
	return nil, nil
}

func (compileGrantStore) Consume(context.Context, string, PrincipalRef) (GrantEnvelope, error) {
	return GrantEnvelope{}, nil
}

func (compileGrantStore) Revoke(context.Context, string, PrincipalRef) (GrantEnvelope, error) {
	return GrantEnvelope{}, nil
}

func (compileGrantStore) Expire(context.Context, time.Time) (int, error) {
	return 0, nil
}

var _ GrantStore = compileGrantStore{}

func TestPhase0ResourceContractsCompile(t *testing.T) {
	grant := GrantEnvelope{
		ID:         "grant-1",
		Principal:  PrincipalRef{Kind: PrincipalWorkTask, ID: "task-1"},
		Capability: CapabilityHostFSRead,
		Resource: ResourceSelector{
			Kind:        ResourceSelectorPath,
			DisplayPath: "@documents/report.txt",
		},
		Operations: []string{OperationRead},
		Constraints: GrantConstraints{
			Recursive: false,
			MaxBytes:  1024,
		},
		Lifetime:    GrantLifetimeOnce,
		Status:      GrantStatusPending,
		BindingHash: "sha256:binding",
		IssuedBy:    GrantIssuedByPolicy,
		CreatedAt:   time.Unix(1, 0).UTC(),
	}
	if grant.Principal.Kind != PrincipalWorkTask || grant.Operations[0] != OperationRead {
		t.Fatalf("grant contract mismatch: %#v", grant)
	}

	decision := PolicyDecision{
		Action:      PolicyActionAsk,
		ReasonCodes: []string{"sensitive_path"},
		RequiredEffects: []Effect{{
			Kind:      EffectHostFSRead,
			Resource:  "documents",
			Sensitive: true,
		}},
		PolicyVersion: "capability-runtime-v0.3",
	}
	if decision.RequiredEffects[0].Kind != EffectHostFSRead {
		t.Fatalf("decision contract mismatch: %#v", decision)
	}
}

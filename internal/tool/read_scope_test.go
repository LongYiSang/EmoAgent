package tool

import (
	"context"
	"testing"
)

func TestReadScopeFromContextDefaultsToWorkspace(t *testing.T) {
	if got := ReadScopeFromContext(nil); got != ReadScopeWorkspace {
		t.Fatalf("nil context read scope = %q, want %q", got, ReadScopeWorkspace)
	}
	if got := ReadScopeFromContext(context.Background()); got != ReadScopeWorkspace {
		t.Fatalf("background read scope = %q, want %q", got, ReadScopeWorkspace)
	}
	if got := ReadScopeFromContext(WithReadScope(context.Background(), "")); got != ReadScopeWorkspace {
		t.Fatalf("empty read scope = %q, want %q", got, ReadScopeWorkspace)
	}
}

func TestReadScopeFromContextRoundTripsAll(t *testing.T) {
	ctx := WithReadScope(context.Background(), ReadScopeAll)
	if got := ReadScopeFromContext(ctx); got != ReadScopeAll {
		t.Fatalf("read scope = %q, want %q", got, ReadScopeAll)
	}
}

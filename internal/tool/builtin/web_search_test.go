package builtin

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin/websearch"
)

// fakeProvider is a test double for websearch.Provider.
type fakeProvider struct {
	lastMaxResults int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Search(_ context.Context, query string, opts websearch.Options) (*websearch.Response, error) {
	f.lastMaxResults = opts.MaxResults
	return &websearch.Response{
		Query: query,
		Results: []websearch.Result{
			{Title: "Fake Result", URL: "https://example.com", Snippet: "A fake snippet."},
		},
	}, nil
}

// --- Schema validation tests ---

func TestWebSearchSpec_SchemaValidation(t *testing.T) {
	validator := tool.MinimalSchemaValidator{}
	schema := WebSearchSpec.Parameters

	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "query only passes",
			input:   `{"query":"hi"}`,
			wantErr: false,
		},
		{
			name:    "query with max_results passes",
			input:   `{"query":"hi","max_results":3}`,
			wantErr: false,
		},
		{
			name:    "query wrong type fails",
			input:   `{"query":123}`,
			wantErr: true,
		},
		{
			name:    "additional property without query fails",
			input:   `{"other":"x"}`,
			wantErr: true,
		},
		{
			name:    "empty object fails (missing required query)",
			input:   `{}`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.Validate(schema, json.RawMessage(tc.input))
			if tc.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

// --- Handler input validation tests ---

func TestWebSearchHandler_Validation(t *testing.T) {
	fake := &fakeProvider{}
	logger := slog.Default()
	const defaultMax = 5
	handler := NewWebSearchHandler(fake, defaultMax, logger)
	ctx := context.Background()

	t.Run("empty query returns error", func(t *testing.T) {
		_, err := handler(ctx, json.RawMessage(`{"query":""}`))
		if err == nil {
			t.Fatal("expected error for empty query")
		}
		if !strings.Contains(err.Error(), "non-empty") {
			t.Errorf("error should mention 'non-empty', got: %v", err)
		}
	})

	t.Run("whitespace-only query returns error", func(t *testing.T) {
		_, err := handler(ctx, json.RawMessage(`{"query":"   "}`))
		if err == nil {
			t.Fatal("expected error for whitespace-only query")
		}
		if !strings.Contains(err.Error(), "non-empty") {
			t.Errorf("error should mention 'non-empty', got: %v", err)
		}
	})

	t.Run("max_results clamped to hard cap", func(t *testing.T) {
		fake.lastMaxResults = 0
		_, err := handler(ctx, json.RawMessage(`{"query":"x","max_results":999}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fake.lastMaxResults != webSearchMaxResultsHardCap {
			t.Errorf("expected MaxResults=%d, got %d", webSearchMaxResultsHardCap, fake.lastMaxResults)
		}
	})

	t.Run("omitted max_results uses defaultMax", func(t *testing.T) {
		fake.lastMaxResults = 0
		_, err := handler(ctx, json.RawMessage(`{"query":"x"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fake.lastMaxResults != defaultMax {
			t.Errorf("expected MaxResults=%d, got %d", defaultMax, fake.lastMaxResults)
		}
	})

	t.Run("max_results zero uses defaultMax", func(t *testing.T) {
		fake.lastMaxResults = 0
		_, err := handler(ctx, json.RawMessage(`{"query":"x","max_results":0}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fake.lastMaxResults != defaultMax {
			t.Errorf("expected MaxResults=%d, got %d", defaultMax, fake.lastMaxResults)
		}
	})
}

package builtin

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin/websearch"
)

// fakeProvider is a test double for websearch.Provider.
type fakeProvider struct {
	lastQuery string
	lastOpts  websearch.Options
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Search(_ context.Context, query string, opts websearch.Options) (*websearch.Response, error) {
	f.lastQuery = query
	f.lastOpts = opts
	return &websearch.Response{
		Query: query,
		Results: []websearch.Result{
			{
				Title:       "Fake Result",
				URL:         "https://example.com",
				Snippet:     "A fake snippet.",
				Score:       0.75,
				RerankScore: 0.91,
				FinalScore:  0.83,
				TrustScore:  0.72,
				Reasons:     []string{"rerank: fake"},
				Evidence: []websearch.EvidenceChunk{{
					Text:   "Evidence body.",
					URL:    "https://example.com",
					Source: "fake",
				}},
				Warnings: []string{"result warning"},
			},
		},
		Warnings: []string{"response warning"},
		Usage:    &websearch.SearchUsage{SearchQueries: 1, ExtractURLs: 1, RerankDocuments: 1},
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
			name:    "phase1 fields pass",
			input:   `{"query":"hi","profile":"official_docs","include_domains":["docs.example.com"],"exclude_domains":["blog.example.com"],"time_range":"week","start_date":"2026-01-01","end_date":"2026-01-31","exact_match":true}`,
			wantErr: false,
		},
		{
			name:    "query wrong type fails",
			input:   `{"query":123}`,
			wantErr: true,
		},
		{
			name:    "unknown field fails",
			input:   `{"query":"hi","unknown":"x"}`,
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

	t.Run("legacy input trims query and preserves legacy output fields", func(t *testing.T) {
		fake.lastQuery = ""
		fake.lastOpts = websearch.Options{}
		raw, err := handler(ctx, json.RawMessage(`{"query":"  test  ","max_results":3}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if fake.lastQuery != "test" {
			t.Fatalf("query = %q, want test", fake.lastQuery)
		}
		if fake.lastOpts.MaxResults != 3 {
			t.Fatalf("MaxResults = %d, want 3", fake.lastOpts.MaxResults)
		}
		if fake.lastOpts.SearchDepth != "" {
			t.Fatalf("SearchDepth = %q, want empty", fake.lastOpts.SearchDepth)
		}
		if len(fake.lastOpts.IncludeDomains) != 0 {
			t.Fatalf("IncludeDomains = %#v, want empty or nil", fake.lastOpts.IncludeDomains)
		}
		if len(fake.lastOpts.ExcludeDomains) != 0 {
			t.Fatalf("ExcludeDomains = %#v, want empty or nil", fake.lastOpts.ExcludeDomains)
		}
		assertOptionStringField(t, fake.lastOpts, "Profile", "")
		assertOptionStringField(t, fake.lastOpts, "Topic", "")
		assertOptionStringField(t, fake.lastOpts, "TimeRange", "")
		assertOptionStringField(t, fake.lastOpts, "StartDate", "")
		assertOptionStringField(t, fake.lastOpts, "EndDate", "")
		assertOptionBoolField(t, fake.lastOpts, "ExactMatch", false)

		var out struct {
			Query    string   `json:"query"`
			Warnings []string `json:"warnings"`
			Usage    *struct {
				SearchQueries int `json:"search_queries"`
				ExtractURLs   int `json:"extract_urls"`
			} `json:"usage"`
			Results []struct {
				Title      string   `json:"title"`
				URL        string   `json:"url"`
				Snippet    string   `json:"snippet"`
				Score      float64  `json:"score"`
				NeedsFetch *bool    `json:"needs_fetch"`
				FetchHint  string   `json:"fetch_hint"`
				Warnings   []string `json:"warnings"`
				Evidence   []struct {
					Text   string `json:"text"`
					URL    string `json:"url"`
					Source string `json:"source"`
				} `json:"evidence"`
			} `json:"results"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if out.Query != "test" {
			t.Fatalf("output query = %q, want test", out.Query)
		}
		if len(out.Results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(out.Results))
		}
		got := out.Results[0]
		if got.Title != "Fake Result" || got.URL != "https://example.com" || got.Snippet != "A fake snippet." || got.Score != 0.75 {
			t.Fatalf("unexpected result fields: %+v", got)
		}
		if got.NeedsFetch == nil {
			t.Fatalf("needs_fetch missing from result JSON: %+v", got)
		}
		if *got.NeedsFetch {
			t.Fatalf("needs_fetch = true, want false when evidence is present")
		}
		if strings.TrimSpace(got.FetchHint) != "" {
			t.Fatalf("fetch_hint = %q, want empty when needs_fetch is false", got.FetchHint)
		}
		if len(got.Evidence) != 1 || got.Evidence[0].Text != "Evidence body." || got.Evidence[0].Source != "fake" {
			t.Fatalf("evidence = %#v, want JSON passthrough", got.Evidence)
		}
		if len(got.Warnings) != 1 || got.Warnings[0] != "result warning" {
			t.Fatalf("result warnings = %#v, want JSON passthrough", got.Warnings)
		}
		if len(out.Warnings) != 1 || out.Warnings[0] != "response warning" {
			t.Fatalf("response warnings = %#v, want JSON passthrough", out.Warnings)
		}
		if out.Usage == nil || out.Usage.SearchQueries != 1 || out.Usage.ExtractURLs != 1 {
			t.Fatalf("usage = %#v, want JSON passthrough", out.Usage)
		}
	})

	t.Run("enhanced rerank fields are passed through with legacy JSON fields", func(t *testing.T) {
		raw, err := handler(ctx, json.RawMessage(`{"query":"enhanced"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var out struct {
			Usage *struct {
				SearchQueries   int `json:"search_queries"`
				ExtractURLs     int `json:"extract_urls"`
				RerankDocuments int `json:"rerank_documents"`
			} `json:"usage"`
			Results []struct {
				Title       string   `json:"title"`
				URL         string   `json:"url"`
				Snippet     string   `json:"snippet"`
				Score       float64  `json:"score"`
				RerankScore float64  `json:"rerank_score"`
				FinalScore  float64  `json:"final_score"`
				TrustScore  float64  `json:"trust_score"`
				Reasons     []string `json:"reasons"`
			} `json:"results"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(out.Results) != 1 {
			t.Fatalf("results = %#v, want 1", out.Results)
		}
		got := out.Results[0]
		if got.Title != "Fake Result" || got.URL != "https://example.com" || got.Snippet != "A fake snippet." || got.Score != 0.75 {
			t.Fatalf("legacy fields = %+v, want preserved", got)
		}
		if got.RerankScore != 0.91 || got.FinalScore != 0.83 || got.TrustScore != 0.72 {
			t.Fatalf("enhanced scores = %+v, want JSON passthrough", got)
		}
		if !reflect.DeepEqual(got.Reasons, []string{"rerank: fake"}) {
			t.Fatalf("reasons = %#v, want passthrough", got.Reasons)
		}
		if out.Usage == nil || out.Usage.RerankDocuments != 1 {
			t.Fatalf("usage = %#v, want rerank_documents passthrough", out.Usage)
		}
	})

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
		fake.lastOpts = websearch.Options{}
		_, err := handler(ctx, json.RawMessage(`{"query":"x","max_results":999}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fake.lastOpts.MaxResults != webSearchMaxResultsHardCap {
			t.Errorf("expected MaxResults=%d, got %d", webSearchMaxResultsHardCap, fake.lastOpts.MaxResults)
		}
	})

	t.Run("omitted max_results uses defaultMax", func(t *testing.T) {
		fake.lastOpts = websearch.Options{}
		_, err := handler(ctx, json.RawMessage(`{"query":"x"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fake.lastOpts.MaxResults != defaultMax {
			t.Errorf("expected MaxResults=%d, got %d", defaultMax, fake.lastOpts.MaxResults)
		}
	})

	t.Run("max_results zero uses defaultMax", func(t *testing.T) {
		fake.lastOpts = websearch.Options{}
		_, err := handler(ctx, json.RawMessage(`{"query":"x","max_results":0}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fake.lastOpts.MaxResults != defaultMax {
			t.Errorf("expected MaxResults=%d, got %d", defaultMax, fake.lastOpts.MaxResults)
		}
	})

	t.Run("phase1 fields are forwarded to websearch options", func(t *testing.T) {
		fake.lastOpts = websearch.Options{}
		_, err := handler(ctx, json.RawMessage(`{
			"query":"golang 2026 release",
			"max_results":4,
			"profile":"auto",
			"include_domains":["go.dev","github.com"],
			"exclude_domains":["old.example.com"],
			"time_range":"month",
			"start_date":"2026-01-01",
			"end_date":"2026-01-31",
			"exact_match":true
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertOptionStringField(t, fake.lastOpts, "Profile", "auto")
		if got := fake.lastOpts.IncludeDomains; !reflect.DeepEqual(got, []string{"go.dev", "github.com"}) {
			t.Fatalf("IncludeDomains = %#v, want go.dev/github.com", got)
		}
		if got := fake.lastOpts.ExcludeDomains; !reflect.DeepEqual(got, []string{"old.example.com"}) {
			t.Fatalf("ExcludeDomains = %#v, want old.example.com", got)
		}
		assertOptionStringField(t, fake.lastOpts, "TimeRange", "month")
		assertOptionStringField(t, fake.lastOpts, "StartDate", "2026-01-01")
		assertOptionStringField(t, fake.lastOpts, "EndDate", "2026-01-31")
		assertOptionBoolField(t, fake.lastOpts, "ExactMatch", true)
	})
}

func assertOptionStringField(t *testing.T, opts websearch.Options, name, want string) {
	t.Helper()
	field := reflect.ValueOf(opts).FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("websearch.Options missing %s", name)
	}
	if field.Kind() != reflect.String {
		t.Fatalf("websearch.Options.%s kind = %s, want string", name, field.Kind())
	}
	if got := field.String(); got != want {
		t.Fatalf("websearch.Options.%s = %q, want %q", name, got, want)
	}
}

func assertOptionBoolField(t *testing.T, opts websearch.Options, name string, want bool) {
	t.Helper()
	field := reflect.ValueOf(opts).FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("websearch.Options missing %s", name)
	}
	if field.Kind() != reflect.Bool {
		t.Fatalf("websearch.Options.%s kind = %s, want bool", name, field.Kind())
	}
	if got := field.Bool(); got != want {
		t.Fatalf("websearch.Options.%s = %v, want %v", name, got, want)
	}
}

package websearch

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestPlannerMapsProfilesAndQueryIntent(t *testing.T) {
	planner := NewPlanner()
	currentYear := strconv.Itoa(time.Now().Year())

	tests := []struct {
		name      string
		query     string
		profile   string
		wantDepth string
		wantTopic string
	}{
		{name: "auto latest chinese", query: "Go 最新 release", profile: "auto", wantDepth: "advanced", wantTopic: "news"},
		{name: "auto today", query: "今天 AI changelog", profile: "auto", wantDepth: "advanced", wantTopic: "news"},
		{name: "auto year", query: "rust " + currentYear + " roadmap", profile: "auto", wantDepth: "advanced", wantTopic: "news"},
		{name: "official docs", query: "go context package", profile: "official_docs", wantDepth: "advanced"},
		{name: "fast", query: "go slices package", profile: "fast", wantDepth: "basic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{}
			setStringOptionForPlanner(t, &opts, "Profile", tt.profile)

			got := planner.Plan(tt.query, opts)
			if got.SearchDepth != tt.wantDepth {
				t.Fatalf("SearchDepth = %q, want %q", got.SearchDepth, tt.wantDepth)
			}
			if tt.wantTopic != "" {
				assertStringOptionForPlanner(t, got, "Topic", tt.wantTopic)
			}
		})
	}
}

func TestPlannerPreservesDomainAndTimeFilters(t *testing.T) {
	planner := NewPlanner()
	opts := Options{
		IncludeDomains: []string{"go.dev"},
		ExcludeDomains: []string{"old.example.com"},
	}
	setStringOptionForPlanner(t, &opts, "Profile", "auto")
	setStringOptionForPlanner(t, &opts, "TimeRange", "month")
	setStringOptionForPlanner(t, &opts, "StartDate", "2026-01-01")
	setStringOptionForPlanner(t, &opts, "EndDate", "2026-01-31")
	setBoolOptionForPlanner(t, &opts, "ExactMatch", true)

	got := planner.Plan("go 2026 release", opts)
	if !reflect.DeepEqual(got.IncludeDomains, opts.IncludeDomains) {
		t.Fatalf("IncludeDomains = %#v, want %#v", got.IncludeDomains, opts.IncludeDomains)
	}
	if !reflect.DeepEqual(got.ExcludeDomains, opts.ExcludeDomains) {
		t.Fatalf("ExcludeDomains = %#v, want %#v", got.ExcludeDomains, opts.ExcludeDomains)
	}
	assertStringOptionForPlanner(t, got, "TimeRange", "month")
	assertStringOptionForPlanner(t, got, "StartDate", "2026-01-01")
	assertStringOptionForPlanner(t, got, "EndDate", "2026-01-31")
	assertBoolOptionForPlanner(t, got, "ExactMatch", true)
}

func setStringOptionForPlanner(t *testing.T, opts *Options, name, value string) {
	t.Helper()
	field := reflect.ValueOf(opts).Elem().FieldByName(name)
	if !field.IsValid() {
		return
	}
	if field.Kind() != reflect.String {
		t.Fatalf("Options.%s kind = %s, want string", name, field.Kind())
	}
	field.SetString(value)
}

func setBoolOptionForPlanner(t *testing.T, opts *Options, name string, value bool) {
	t.Helper()
	field := reflect.ValueOf(opts).Elem().FieldByName(name)
	if !field.IsValid() {
		return
	}
	if field.Kind() != reflect.Bool {
		t.Fatalf("Options.%s kind = %s, want bool", name, field.Kind())
	}
	field.SetBool(value)
}

func assertStringOptionForPlanner(t *testing.T, opts Options, name, want string) {
	t.Helper()
	field := reflect.ValueOf(opts).FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("Options missing %s", name)
	}
	if field.Kind() != reflect.String {
		t.Fatalf("Options.%s kind = %s, want string", name, field.Kind())
	}
	if got := field.String(); got != want {
		t.Fatalf("Options.%s = %q, want %q", name, got, want)
	}
}

func assertBoolOptionForPlanner(t *testing.T, opts Options, name string, want bool) {
	t.Helper()
	field := reflect.ValueOf(opts).FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("Options missing %s", name)
	}
	if field.Kind() != reflect.Bool {
		t.Fatalf("Options.%s kind = %s, want bool", name, field.Kind())
	}
	if got := field.Bool(); got != want {
		t.Fatalf("Options.%s = %v, want %v", name, got, want)
	}
}

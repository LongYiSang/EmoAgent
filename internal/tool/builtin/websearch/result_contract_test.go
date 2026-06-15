package websearch

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestResultDeclaresFetchGuidanceJSONFields(t *testing.T) {
	resultType := reflect.TypeOf(Result{})

	needsFetch, ok := resultType.FieldByName("NeedsFetch")
	if !ok {
		t.Fatal("Result missing NeedsFetch field for needs_fetch JSON contract")
	}
	if needsFetch.Type.Kind() != reflect.Bool {
		t.Fatalf("NeedsFetch type = %s, want bool", needsFetch.Type)
	}
	assertJSONFieldName(t, needsFetch, "needs_fetch")
	if strings.Contains(needsFetch.Tag.Get("json"), "omitempty") {
		t.Fatalf("NeedsFetch json tag = %q, want needs_fetch to be emitted even when false", needsFetch.Tag.Get("json"))
	}

	fetchHint, ok := resultType.FieldByName("FetchHint")
	if !ok {
		t.Fatal("Result missing FetchHint field for fetch_hint JSON contract")
	}
	if fetchHint.Type.Kind() != reflect.String {
		t.Fatalf("FetchHint type = %s, want string", fetchHint.Type)
	}
	assertJSONFieldName(t, fetchHint, "fetch_hint")
}

func assertJSONFieldName(t *testing.T, field reflect.StructField, want string) {
	t.Helper()
	got, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if got != want {
		t.Fatalf("%s json tag = %q, want %q", field.Name, field.Tag.Get("json"), want)
	}
}

func assertResultFetchGuidance(t *testing.T, result Result, wantNeedsFetch bool, wantHintParts ...string) {
	t.Helper()

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal result JSON: %v", err)
	}

	rawNeedsFetch, ok := fields["needs_fetch"]
	if !ok {
		t.Fatalf("result JSON = %s, missing needs_fetch", raw)
	}
	gotNeedsFetch, ok := rawNeedsFetch.(bool)
	if !ok {
		t.Fatalf("needs_fetch = %#v, want bool in result JSON %s", rawNeedsFetch, raw)
	}
	if gotNeedsFetch != wantNeedsFetch {
		t.Fatalf("needs_fetch = %v, want %v in result JSON %s", gotNeedsFetch, wantNeedsFetch, raw)
	}

	var fetchHint string
	if rawFetchHint, ok := fields["fetch_hint"]; ok {
		gotFetchHint, ok := rawFetchHint.(string)
		if !ok {
			t.Fatalf("fetch_hint = %#v, want string in result JSON %s", rawFetchHint, raw)
		}
		fetchHint = gotFetchHint
	}

	if len(wantHintParts) == 0 {
		if strings.TrimSpace(fetchHint) != "" {
			t.Fatalf("fetch_hint = %q, want empty when needs_fetch is false", fetchHint)
		}
		return
	}
	if strings.TrimSpace(fetchHint) == "" {
		t.Fatalf("fetch_hint is empty, want it to explain needs_fetch in result JSON %s", raw)
	}
	lowerHint := strings.ToLower(fetchHint)
	for _, part := range wantHintParts {
		if !strings.Contains(lowerHint, strings.ToLower(part)) {
			t.Fatalf("fetch_hint = %q, want to contain %q", fetchHint, part)
		}
	}
}

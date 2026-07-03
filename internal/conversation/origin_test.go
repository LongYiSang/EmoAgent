package conversation

import "testing"

func TestOriginResolverDefaultsToWebUIMain(t *testing.T) {
	origin, err := ResolveOrigin(OriginRequest{})
	if err != nil {
		t.Fatalf("ResolveOrigin: %v", err)
	}
	if origin.OriginKey != "webui:local:main" {
		t.Fatalf("OriginKey = %q, want webui:local:main", origin.OriginKey)
	}
	if origin.SourceType != "webui" || origin.ChannelType != "web" {
		t.Fatalf("origin = %#v, want webui/web defaults", origin)
	}
}

func TestValidateOriginKeyRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{"", "webui local main", "../main", "webui:local:main"} {
		err := ValidateOriginKey(value)
		if value == "webui:local:main" && err != nil {
			t.Fatalf("ValidateOriginKey(%q): %v", value, err)
		}
		if value != "webui:local:main" && err == nil {
			t.Fatalf("ValidateOriginKey(%q) err = nil, want error", value)
		}
	}
}

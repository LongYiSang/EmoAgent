package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

func TestIndexStaticIsViteEntrypoint(t *testing.T) {
	requireEmbeddedDist(t)
	assertViteEntrypoint(t, "static/dist/index.html", "/assets/index-")
}

func assertViteEntrypoint(t *testing.T, path string, entryPrefix string) {
	t.Helper()

	data, err := StaticFS.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	html := string(data)

	for _, want := range []string{
		`<div id="root"></div>`,
		`type="module"`,
		entryPrefix,
		`/assets/styles-`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("%s missing %q", path, want)
		}
	}

	for _, legacy := range []string{
		"legacy-static-markers",
		"legacy-admin-markers",
		"Legacy static parity markers",
		"/src/chat/main.tsx",
		"/src/admin/main.tsx",
		"let memoryStatusVisible = false",
	} {
		if strings.Contains(html, legacy) {
			t.Fatalf("%s still contains legacy marker %q", path, legacy)
		}
	}

	assetRE := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`)
	matches := assetRE.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		t.Fatalf("%s has no referenced Vite assets", path)
	}
	for _, match := range matches {
		if _, err := StaticFS.ReadFile("static/dist" + match[1]); err != nil {
			t.Fatalf("%s references missing asset %s: %v", path, match[1], err)
		}
	}
}

func TestStaticHandlerWithoutBuildReportsRequiredStep(t *testing.T) {
	handler := NewStaticHandler(fstest.MapFS{
		"static/README.md": {Data: []byte("placeholder")},
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rec.Body.String(), "npm --prefix web run build") {
		t.Fatalf("body = %q, want build instruction", rec.Body.String())
	}
}

func TestStaticRootHasNoGeneratedBuildArtifacts(t *testing.T) {
	for _, path := range []string{
		"static/index.html",
		"static/admin.html",
		"static/assets",
	} {
		if _, err := fs.Stat(StaticFS, path); err == nil {
			t.Fatalf("generated build artifact still embedded at %s", path)
		}
	}
}

func TestIndexStaticHasNoOldStandaloneCSS(t *testing.T) {
	requireEmbeddedDist(t)
	for _, path := range []string{
		"static/dist/shared.css",
	} {
		if _, err := StaticFS.ReadFile(path); err == nil {
			t.Fatalf("legacy static file still embedded: %s", path)
		}
	}
}

func requireEmbeddedDist(t *testing.T) {
	t.Helper()
	if _, err := fs.Stat(StaticFS, "static/dist/index.html"); err != nil {
		t.Skip("embedded frontend dist is absent; run npm --prefix web run build before go test for release asset checks")
	}
}

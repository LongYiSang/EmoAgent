package web

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/logcenter"
)

type logCenterFakeApp struct {
	fakeAdminApp
	sources   []logcenter.Source
	events    []logcenter.Event
	stream    <-chan logcenter.Event
	lastQuery logcenter.Query
}

func (f *logCenterFakeApp) ListLogSources(context.Context) ([]logcenter.Source, error) {
	return append([]logcenter.Source(nil), f.sources...), nil
}

func (f *logCenterFakeApp) ListLogEvents(_ context.Context, query logcenter.Query) ([]logcenter.Event, error) {
	f.lastQuery = query
	return append([]logcenter.Event(nil), f.events...), nil
}

func (f *logCenterFakeApp) SubscribeLogEvents(_ context.Context, query logcenter.Query) (<-chan logcenter.Event, func(), error) {
	f.lastQuery = query
	if f.stream != nil {
		return f.stream, func() {}, nil
	}
	ch := make(chan logcenter.Event)
	close(ch)
	return ch, func() {}, nil
}

func TestHandleListLogSources(t *testing.T) {
	app := &logCenterFakeApp{sources: []logcenter.Source{{
		Type:   logcenter.SourceTypeSidecar,
		ID:     "memorycore",
		Label:  "MemoryCore Sidecar",
		Status: logcenter.SourceStatusUnavailable,
	}}}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()

	handler.HandleListLogSources(rec, httptest.NewRequest(http.MethodGet, "/api/logs/sources", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var body struct {
		Sources []logcenter.Source `json:"sources"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body.Sources) != 1 || body.Sources[0].ID != "memorycore" {
		t.Fatalf("sources = %#v, want memorycore", body.Sources)
	}
}

func TestHandleListLogEventsParsesQuery(t *testing.T) {
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	app := &logCenterFakeApp{events: []logcenter.Event{{
		ID:         "9",
		Time:       now,
		SourceType: logcenter.SourceTypePlugin,
		SourceID:   "demo",
		Level:      logcenter.LevelWarn,
		Message:    "warn",
	}}}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/logs/events?source_type=plugin&source_id=demo&min_level=warn&after_id=8&limit=3", nil)
	rec := httptest.NewRecorder()

	handler.HandleListLogEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if app.lastQuery.SourceType != logcenter.SourceTypePlugin ||
		app.lastQuery.SourceID != "demo" ||
		app.lastQuery.MinLevel != logcenter.LevelWarn ||
		app.lastQuery.AfterID != "8" ||
		app.lastQuery.Limit != 3 {
		t.Fatalf("query = %#v", app.lastQuery)
	}
	if !strings.Contains(rec.Body.String(), `"message":"warn"`) {
		t.Fatalf("body = %s, want warning event", rec.Body.String())
	}
}

func TestHandleListLogEventsRejectsSourceIDWithoutType(t *testing.T) {
	handler := NewAPIHandler(&logCenterFakeApp{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()

	handler.HandleListLogEvents(rec, httptest.NewRequest(http.MethodGet, "/api/logs/events?source_id=demo", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestHandleStreamLogEventsWritesSSEReplay(t *testing.T) {
	stream := make(chan logcenter.Event)
	close(stream)
	app := &logCenterFakeApp{
		events: []logcenter.Event{{
			ID:         "2",
			Time:       time.Date(2026, 7, 7, 10, 0, 1, 0, time.UTC),
			SourceType: logcenter.SourceTypeMain,
			SourceID:   "host",
			Level:      logcenter.LevelInfo,
			Message:    "ready",
		}},
		stream: stream,
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream?limit=10", nil)
	req.Header.Set("Last-Event-ID", "1")
	rec := httptest.NewRecorder()

	handler.HandleStreamLogEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if app.lastQuery.AfterID != "1" {
		t.Fatalf("after_id = %q, want Last-Event-ID", app.lastQuery.AfterID)
	}
	body := rec.Body.String()
	for _, want := range []string{"id: 2", "event: log", `"message":"ready"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s, want %s", body, want)
		}
	}
}

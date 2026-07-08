package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/longyisang/emoagent/internal/logcenter"
)

const (
	defaultLogEventsLimit = 500
	maxLogEventsLimit     = 2000
)

type LogCenterApp interface {
	ListLogSources(context.Context) ([]logcenter.Source, error)
	ListLogEvents(context.Context, logcenter.Query) ([]logcenter.Event, error)
	SubscribeLogEvents(context.Context, logcenter.Query) (<-chan logcenter.Event, func(), error)
}

func (h *APIHandler) logCenterApp(w http.ResponseWriter) (LogCenterApp, bool) {
	app, ok := any(h.app).(LogCenterApp)
	if !ok {
		writeError(w, http.StatusNotImplemented, "log center API is not available")
		return nil, false
	}
	return app, true
}

func (h *APIHandler) HandleListLogSources(w http.ResponseWriter, r *http.Request) {
	app, ok := h.logCenterApp(w)
	if !ok {
		return
	}
	sources, err := app.ListLogSources(r.Context())
	if err != nil {
		h.logger.Error("log sources internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (h *APIHandler) HandleListLogEvents(w http.ResponseWriter, r *http.Request) {
	app, ok := h.logCenterApp(w)
	if !ok {
		return
	}
	query, ok := parseLogQuery(w, r)
	if !ok {
		return
	}
	events, err := app.ListLogEvents(r.Context(), query)
	if err != nil {
		h.logger.Error("log events internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *APIHandler) HandleStreamLogEvents(w http.ResponseWriter, r *http.Request) {
	app, ok := h.logCenterApp(w)
	if !ok {
		return
	}
	query, ok := parseLogQuery(w, r)
	if !ok {
		return
	}
	if lastEventID := strings.TrimSpace(r.Header.Get("Last-Event-ID")); lastEventID != "" {
		query.AfterID = lastEventID
	}
	events, cancel, err := app.SubscribeLogEvents(r.Context(), query)
	if err != nil {
		h.logger.Error("log stream internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	initial, err := app.ListLogEvents(r.Context(), query)
	if err != nil {
		cancel()
		h.logger.Error("log stream replay internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		cancel()
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	for _, event := range initial {
		if err := writeLogEvent(w, event); err != nil {
			return
		}
	}
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeLogEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func parseLogQuery(w http.ResponseWriter, r *http.Request) (logcenter.Query, bool) {
	values := r.URL.Query()
	query := logcenter.Query{
		Limit:   defaultLogEventsLimit,
		AfterID: strings.TrimSpace(values.Get("after_id")),
	}
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return logcenter.Query{}, false
		}
		if limit > maxLogEventsLimit {
			limit = maxLogEventsLimit
		}
		query.Limit = limit
	}
	if sourceType, ok := parseLogSourceType(w, values.Get("source_type")); !ok {
		return logcenter.Query{}, false
	} else {
		query.SourceType = sourceType
	}
	query.SourceID = strings.TrimSpace(values.Get("source_id"))
	if query.SourceID != "" && query.SourceType == "" {
		writeError(w, http.StatusBadRequest, "source_type is required when source_id is set")
		return logcenter.Query{}, false
	}
	if minLevel, ok := parseLogLevel(w, values.Get("min_level")); !ok {
		return logcenter.Query{}, false
	} else {
		query.MinLevel = minLevel
	}
	return query, true
}

func parseLogSourceType(w http.ResponseWriter, value string) (logcenter.SourceType, bool) {
	switch strings.TrimSpace(value) {
	case "":
		return "", true
	case string(logcenter.SourceTypeMain):
		return logcenter.SourceTypeMain, true
	case string(logcenter.SourceTypeSidecar):
		return logcenter.SourceTypeSidecar, true
	case string(logcenter.SourceTypePlugin):
		return logcenter.SourceTypePlugin, true
	default:
		writeError(w, http.StatusBadRequest, "invalid source_type")
		return "", false
	}
}

func parseLogLevel(w http.ResponseWriter, value string) (logcenter.Level, bool) {
	switch strings.TrimSpace(value) {
	case "":
		return "", true
	case string(logcenter.LevelDebug):
		return logcenter.LevelDebug, true
	case string(logcenter.LevelInfo):
		return logcenter.LevelInfo, true
	case string(logcenter.LevelWarn):
		return logcenter.LevelWarn, true
	case string(logcenter.LevelError):
		return logcenter.LevelError, true
	default:
		writeError(w, http.StatusBadRequest, "invalid min_level")
		return "", false
	}
}

func writeLogEvent(w http.ResponseWriter, event logcenter.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %s\nevent: log\ndata: %s\n\n", event.ID, data)
	return err
}

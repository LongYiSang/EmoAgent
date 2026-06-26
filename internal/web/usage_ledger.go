package web

import (
	"net/http"
	"strings"

	"github.com/longyisang/emoagent/internal/storage"
)

func (h *APIHandler) HandleListLLMUsageEvents(w http.ResponseWriter, r *http.Request) {
	events, err := h.app.ListLLMUsageEvents(r.Context(), usageEventFilterFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list llm usage events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *APIHandler) HandleSummarizeLLMUsage(w http.ResponseWriter, r *http.Request) {
	filter := storage.LLMUsageSummaryFilter{
		LLMUsageEventFilter: usageEventFilterFromRequest(r),
		GroupBy:             queryList(r, "group_by"),
	}
	if len(filter.GroupBy) == 0 {
		filter.GroupBy = []string{"provider", "model", "component"}
	}
	rows, err := h.app.SummarizeLLMUsage(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

func (h *APIHandler) HandleListTokenEstimatorCalibrations(w http.ResponseWriter, r *http.Request) {
	calibrations, err := h.app.ListTokenEstimatorCalibrations(r.Context(), calibrationFilterFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list token estimator calibrations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"calibrations": calibrations})
}

func (h *APIHandler) HandleRefreshTokenEstimatorCalibrations(w http.ResponseWriter, r *http.Request) {
	count, err := h.app.RefreshTokenEstimatorCalibrations(r.Context(), calibrationFilterFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to refresh token estimator calibrations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refreshed": count})
}

func usageEventFilterFromRequest(r *http.Request) storage.LLMUsageEventFilter {
	limit := queryInt(r, "limit", 100)
	if limit > 1000 {
		limit = 1000
	}
	q := r.URL.Query()
	return storage.LLMUsageEventFilter{
		SessionID:   strings.TrimSpace(q.Get("session_id")),
		ProviderID:  strings.TrimSpace(q.Get("provider_id")),
		Model:       strings.TrimSpace(q.Get("model")),
		Component:   strings.TrimSpace(q.Get("component")),
		Operation:   strings.TrimSpace(q.Get("operation")),
		PluginID:    strings.TrimSpace(q.Get("plugin_id")),
		TaskID:      strings.TrimSpace(q.Get("task_id")),
		UsageSource: strings.TrimSpace(q.Get("usage_source")),
		Status:      strings.TrimSpace(q.Get("status")),
		From:        strings.TrimSpace(q.Get("from")),
		To:          strings.TrimSpace(q.Get("to")),
		Limit:       limit,
	}
}

func calibrationFilterFromRequest(r *http.Request) storage.TokenEstimatorCalibrationFilter {
	q := r.URL.Query()
	return storage.TokenEstimatorCalibrationFilter{
		ProviderID:     strings.TrimSpace(q.Get("provider_id")),
		Model:          strings.TrimSpace(q.Get("model")),
		EstimateMethod: strings.TrimSpace(q.Get("estimate_method")),
		Bucket:         strings.TrimSpace(q.Get("bucket")),
	}
}

func queryList(r *http.Request, key string) []string {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

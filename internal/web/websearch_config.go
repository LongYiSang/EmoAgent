package web

import (
	"net/http"

	"github.com/longyisang/emoagent/internal/config"
)

type webSearchConfigRequest struct {
	WebSearch config.WebSearchConfig `json:"websearch"`
}

func (h *APIHandler) HandleGetWebSearchConfig(w http.ResponseWriter, r *http.Request) {
	resp, err := h.app.GetWebSearchConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleUpdateWebSearchConfig(w http.ResponseWriter, r *http.Request) {
	var req webSearchConfigRequest
	if err := readJSON(w, r, &req); err != nil {
		writeJSONReadError(w, err)
		return
	}
	resp, err := h.app.UpdateWebSearchConfig(r.Context(), req.WebSearch)
	if err != nil {
		h.writeConfigMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

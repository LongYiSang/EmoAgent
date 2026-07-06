package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/pytoolchain"
)

type PythonToolchainAdminApp interface {
	GetPythonToolchainConfig(ctx context.Context) (config.PythonToolchainConfig, error)
	ProbePythonToolchain(ctx context.Context, cfg config.PythonToolchainConfig) (pytoolchain.ProbeResult, error)
	UpdatePythonToolchainConfig(ctx context.Context, cfg config.PythonToolchainConfig) (configcenter.EffectiveConfig, error)
	ListPythonEnvironments(ctx context.Context) ([]pytoolchain.EnvironmentSummary, error)
	SyncPythonEnvironment(ctx context.Context, kind, id, version string) (pytoolchain.EnvironmentSummary, error)
	RepairPythonEnvironment(ctx context.Context, kind, id, version string) (pytoolchain.EnvironmentSummary, error)
}

type pythonToolchainConfigResponse struct {
	PythonToolchain config.PythonToolchainConfig `json:"python_toolchain"`
}

type pythonToolchainProbeRequest struct {
	PythonToolchain config.PythonToolchainConfig `json:"python_toolchain"`
}

type pythonToolchainProbeResponse struct {
	Result pytoolchain.ProbeResult `json:"result"`
}

type pythonToolchainUpdateResponse struct {
	PythonToolchain config.PythonToolchainConfig `json:"python_toolchain"`
	EffectiveConfig configcenter.EffectiveConfig `json:"effective_config"`
	RestartRequired bool                         `json:"restart_required"`
}

type pythonEnvironmentListResponse struct {
	Environments []pytoolchain.EnvironmentSummary `json:"environments"`
}

type pythonEnvironmentResponse struct {
	Environment pytoolchain.EnvironmentSummary `json:"environment"`
}

func (h *APIHandler) HandleGetPythonToolchain(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pythonToolchainApp(w)
	if !ok {
		return
	}
	cfg, err := app.GetPythonToolchainConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pythonToolchainConfigResponse{PythonToolchain: cfg})
}

func (h *APIHandler) HandleProbePythonToolchain(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pythonToolchainApp(w)
	if !ok {
		return
	}
	var req pythonToolchainProbeRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := readJSON(w, r, &req); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
			writeJSONReadError(w, err)
			return
		}
	}
	cfg := req.PythonToolchain
	if cfg.PythonExecutable == "" && cfg.UVExecutable == "" {
		current, err := app.GetPythonToolchainConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cfg = current
	}
	result, err := app.ProbePythonToolchain(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pythonToolchainProbeResponse{Result: result})
}

func (h *APIHandler) HandleUpdatePythonToolchain(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pythonToolchainApp(w)
	if !ok {
		return
	}
	var req pythonToolchainProbeRequest
	if err := readJSON(w, r, &req); err != nil {
		writeJSONReadError(w, err)
		return
	}
	effective, err := app.UpdatePythonToolchainConfig(r.Context(), req.PythonToolchain)
	if err != nil {
		h.writeConfigMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pythonToolchainUpdateResponse{
		PythonToolchain: effective.PythonToolchain,
		EffectiveConfig: effective,
		RestartRequired: true,
	})
}

func (h *APIHandler) HandleListPythonEnvironments(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pythonToolchainApp(w)
	if !ok {
		return
	}
	environments, err := app.ListPythonEnvironments(r.Context())
	if err != nil {
		h.logger.Error("python environments internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, pythonEnvironmentListResponse{Environments: environments})
}

func (h *APIHandler) HandleSyncPythonEnvironment(w http.ResponseWriter, r *http.Request) {
	h.handleEnsurePythonEnvironment(w, r, false)
}

func (h *APIHandler) HandleRepairPythonEnvironment(w http.ResponseWriter, r *http.Request) {
	h.handleEnsurePythonEnvironment(w, r, true)
}

func (h *APIHandler) handleEnsurePythonEnvironment(w http.ResponseWriter, r *http.Request, repair bool) {
	app, ok := h.pythonToolchainApp(w)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	id := r.PathValue("id")
	version := r.URL.Query().Get("version")
	var (
		environment pytoolchain.EnvironmentSummary
		err         error
	)
	if repair {
		environment, err = app.RepairPythonEnvironment(r.Context(), kind, id, version)
	} else {
		environment, err = app.SyncPythonEnvironment(r.Context(), kind, id, version)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pythonEnvironmentResponse{Environment: environment})
}

func (h *APIHandler) pythonToolchainApp(w http.ResponseWriter) (PythonToolchainAdminApp, bool) {
	app, ok := any(h.app).(PythonToolchainAdminApp)
	if !ok {
		writeError(w, http.StatusNotFound, "python toolchain admin API is unavailable")
		return nil, false
	}
	return app, true
}

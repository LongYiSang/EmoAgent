package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/pytoolchain"
)

type pythonToolchainAPIFakeApp struct {
	fakeAdminApp
	cfg           config.PythonToolchainConfig
	updateIn      config.PythonToolchainConfig
	probeIn       config.PythonToolchainConfig
	probeOut      pytoolchain.ProbeResult
	environments  []pytoolchain.EnvironmentSummary
	syncKind      string
	syncID        string
	syncVersion   string
	repairKind    string
	repairID      string
	repairVersion string
}

func (a *pythonToolchainAPIFakeApp) GetPythonToolchainConfig(context.Context) (config.PythonToolchainConfig, error) {
	return a.cfg, nil
}

func (a *pythonToolchainAPIFakeApp) ProbePythonToolchain(_ context.Context, cfg config.PythonToolchainConfig) (pytoolchain.ProbeResult, error) {
	a.probeIn = cfg
	return a.probeOut, nil
}

func (a *pythonToolchainAPIFakeApp) UpdatePythonToolchainConfig(_ context.Context, cfg config.PythonToolchainConfig) (configcenter.EffectiveConfig, error) {
	a.updateIn = cfg
	a.cfg = cfg
	return configcenter.EffectiveConfig{PythonToolchain: cfg}, nil
}

func (a *pythonToolchainAPIFakeApp) ListPythonEnvironments(context.Context) ([]pytoolchain.EnvironmentSummary, error) {
	return append([]pytoolchain.EnvironmentSummary(nil), a.environments...), nil
}

func (a *pythonToolchainAPIFakeApp) SyncPythonEnvironment(_ context.Context, kind, id, version string) (pytoolchain.EnvironmentSummary, error) {
	a.syncKind, a.syncID, a.syncVersion = kind, id, version
	return a.environments[0], nil
}

func (a *pythonToolchainAPIFakeApp) RepairPythonEnvironment(_ context.Context, kind, id, version string) (pytoolchain.EnvironmentSummary, error) {
	a.repairKind, a.repairID, a.repairVersion = kind, id, version
	return a.environments[0], nil
}

func TestPythonToolchainAPIGetAndProbe(t *testing.T) {
	app := &pythonToolchainAPIFakeApp{
		cfg: config.PythonToolchainConfig{
			Enabled:          true,
			PythonExecutable: "C:/Python312/python.exe",
			UVExecutable:     "C:/Users/me/.local/bin/uv.exe",
			RequiredPython:   "3.12",
		},
		probeOut: pytoolchain.ProbeResult{
			Python: pytoolchain.PythonProbeResult{Version: "3.12.11"},
			UV:     pytoolchain.UVProbeResult{Version: "0.11.9"},
		},
	}
	handler := NewAPIHandler(any(app).(AdminApp), slog.New(slog.NewTextHandler(io.Discard, nil)))

	getRec := httptest.NewRecorder()
	handler.HandleGetPythonToolchain(getRec, httptest.NewRequest(http.MethodGet, "/api/python-toolchain", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var getBody struct {
		PythonToolchain config.PythonToolchainConfig `json:"python_toolchain"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if getBody.PythonToolchain.PythonExecutable != "C:/Python312/python.exe" {
		t.Fatalf("GET body = %#v", getBody)
	}

	probeRec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"python_toolchain":{"python_executable":"C:/Python312/python.exe","uv_executable":"C:/Users/me/.local/bin/uv.exe"}}`)
	handler.HandleProbePythonToolchain(probeRec, httptest.NewRequest(http.MethodPost, "/api/python-toolchain/probe", body))
	if probeRec.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", probeRec.Code, probeRec.Body.String())
	}
	if app.probeIn.PythonExecutable != "C:/Python312/python.exe" || app.probeIn.UVExecutable != "C:/Users/me/.local/bin/uv.exe" {
		t.Fatalf("probe input = %#v", app.probeIn)
	}
	var probeBody struct {
		Result pytoolchain.ProbeResult `json:"result"`
	}
	if err := json.Unmarshal(probeRec.Body.Bytes(), &probeBody); err != nil {
		t.Fatalf("decode POST: %v", err)
	}
	if probeBody.Result.Python.Version != "3.12.11" || probeBody.Result.UV.Version != "0.11.9" {
		t.Fatalf("probe body = %#v", probeBody)
	}
}

func TestPythonToolchainAPIUpdateAndEnvironmentActions(t *testing.T) {
	app := &pythonToolchainAPIFakeApp{
		environments: []pytoolchain.EnvironmentSummary{{
			Owner: pytoolchain.EnvironmentOwner{
				Kind:    pytoolchain.OwnerPlugin,
				ID:      "com.example.managed",
				Version: "0.1.0",
				EnvDir:  "C:/envs/plugins/com.example.managed/0.1.0",
			},
			Status:  pytoolchain.EnvironmentStatus{State: pytoolchain.EnvReady},
			Enabled: true,
		}},
	}
	handler := NewAPIHandler(any(app).(AdminApp), slog.New(slog.NewTextHandler(io.Discard, nil)))

	updateBody := bytes.NewBufferString(`{"python_toolchain":{"enabled":true,"python_executable":"C:/Python312/python.exe","uv_executable":"C:/Users/me/.local/bin/uv.exe","required_python":"3.12","minimum_uv_version":"0.11.0","environment_root":"C:/envs","cache_dir":"C:/cache","default_index":"https://pypi.org/simple","sync_timeout_seconds":600,"use_system_certificates":false}}`)
	updateRec := httptest.NewRecorder()
	handler.HandleUpdatePythonToolchain(updateRec, httptest.NewRequest(http.MethodPut, "/api/python-toolchain", updateBody))
	if updateRec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}
	if app.updateIn.PythonExecutable != "C:/Python312/python.exe" || app.updateIn.UseSystemCertificates {
		t.Fatalf("update input = %#v", app.updateIn)
	}
	var updateResp struct {
		RestartRequired bool `json:"restart_required"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	if !updateResp.RestartRequired {
		t.Fatalf("restart_required = false")
	}

	listRec := httptest.NewRecorder()
	handler.HandleListPythonEnvironments(listRec, httptest.NewRequest(http.MethodGet, "/api/python-toolchain/environments", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Environments []pytoolchain.EnvironmentSummary `json:"environments"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Environments) != 1 || listResp.Environments[0].Status.State != pytoolchain.EnvReady {
		t.Fatalf("list body = %#v", listResp)
	}

	syncReq := httptest.NewRequest(http.MethodPost, "/api/python-toolchain/environments/plugin/com.example.managed/sync?version=0.1.0", nil)
	syncReq.SetPathValue("kind", "plugin")
	syncReq.SetPathValue("id", "com.example.managed")
	syncRec := httptest.NewRecorder()
	handler.HandleSyncPythonEnvironment(syncRec, syncReq)
	if syncRec.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", syncRec.Code, syncRec.Body.String())
	}
	if app.syncKind != "plugin" || app.syncID != "com.example.managed" || app.syncVersion != "0.1.0" {
		t.Fatalf("sync args = %q %q %q", app.syncKind, app.syncID, app.syncVersion)
	}

	repairReq := httptest.NewRequest(http.MethodPost, "/api/python-toolchain/environments/plugin/com.example.managed/repair?version=0.1.0", nil)
	repairReq.SetPathValue("kind", "plugin")
	repairReq.SetPathValue("id", "com.example.managed")
	repairRec := httptest.NewRecorder()
	handler.HandleRepairPythonEnvironment(repairRec, repairReq)
	if repairRec.Code != http.StatusOK {
		t.Fatalf("repair status=%d body=%s", repairRec.Code, repairRec.Body.String())
	}
	if app.repairKind != "plugin" || app.repairID != "com.example.managed" || app.repairVersion != "0.1.0" {
		t.Fatalf("repair args = %q %q %q", app.repairKind, app.repairID, app.repairVersion)
	}
}

package app

import (
	"context"

	"github.com/longyisang/emoagent/internal/plugin"
)

type SelfTestReport struct {
	Status            string                        `json:"status"`
	PluginDiagnostics plugin.AdminPluginDiagnostics `json:"plugin_diagnostics"`
}

func (a *App) SelfTest(ctx context.Context) (SelfTestReport, error) {
	diagnostics, err := a.PluginDiagnostics(ctx)
	if err != nil {
		return SelfTestReport{}, err
	}
	return SelfTestReport{
		Status:            diagnostics.Status,
		PluginDiagnostics: diagnostics,
	}, nil
}

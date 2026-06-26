package app

import (
	"context"

	"github.com/longyisang/emoagent/internal/storage"
)

func (a *App) ListLLMUsageEvents(ctx context.Context, filter storage.LLMUsageEventFilter) ([]storage.LLMUsageEvent, error) {
	kernel, err := a.kernelSnapshot()
	if err != nil {
		return nil, err
	}
	return kernel.Infra.DB.ListLLMUsageEvents(ctx, filter)
}

func (a *App) SummarizeLLMUsage(ctx context.Context, filter storage.LLMUsageSummaryFilter) ([]storage.LLMUsageSummaryRow, error) {
	kernel, err := a.kernelSnapshot()
	if err != nil {
		return nil, err
	}
	return kernel.Infra.DB.SummarizeLLMUsage(ctx, filter)
}

func (a *App) ListTokenEstimatorCalibrations(ctx context.Context, filter storage.TokenEstimatorCalibrationFilter) ([]storage.TokenEstimatorCalibration, error) {
	kernel, err := a.kernelSnapshot()
	if err != nil {
		return nil, err
	}
	return kernel.Infra.DB.ListTokenEstimatorCalibrations(ctx, filter)
}

func (a *App) RefreshTokenEstimatorCalibrations(ctx context.Context, filter storage.TokenEstimatorCalibrationFilter) (int, error) {
	kernel, err := a.kernelSnapshot()
	if err != nil {
		return 0, err
	}
	return kernel.Infra.DB.RefreshTokenEstimatorCalibrations(ctx, filter)
}

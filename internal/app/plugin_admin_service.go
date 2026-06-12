package app

import (
	"context"

	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
)

func (a *App) InstallLocalPlugin(ctx context.Context, req plugin.AdminPluginInstallRequest) (plugin.AdminPluginSummary, error) {
	services, err := a.services()
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	return services.Plugins.InstallLocal(ctx, req)
}

func (a *App) InstallGitHubPluginRelease(ctx context.Context, req plugin.AdminGitHubInstallRequest) (plugin.AdminPluginSummary, error) {
	services, err := a.services()
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	return services.Plugins.InstallGitHubRelease(ctx, req)
}

func (a *App) ListPlugins(ctx context.Context) ([]plugin.AdminPluginSummary, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Plugins.ListPlugins(ctx)
}

func (a *App) GetPlugin(ctx context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	services, err := a.services()
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	return services.Plugins.GetPlugin(ctx, pluginID)
}

func (a *App) EnablePlugin(ctx context.Context, pluginID string, req plugin.AdminPluginEnableRequest) (plugin.AdminPluginSummary, error) {
	services, err := a.services()
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	return services.Plugins.EnablePlugin(ctx, pluginID, req)
}

func (a *App) DisablePlugin(ctx context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	services, err := a.services()
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	return services.Plugins.DisablePlugin(ctx, pluginID)
}

func (a *App) RestartPlugin(ctx context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	services, err := a.services()
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	return services.Plugins.RestartPlugin(ctx, pluginID)
}

func (a *App) DeletePlugin(ctx context.Context, pluginID string) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.Plugins.DeletePlugin(ctx, pluginID)
}

func (a *App) PluginLogs(ctx context.Context, pluginID string) (plugin.AdminPluginLogs, error) {
	services, err := a.services()
	if err != nil {
		return plugin.AdminPluginLogs{}, err
	}
	return services.Plugins.PluginLogs(ctx, pluginID)
}

func (a *App) ListPluginAccessEvents(ctx context.Context, pluginID string, limit int) ([]storage.PluginAccessEvent, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Plugins.ListPluginAccessEvents(ctx, pluginID, limit)
}

func (a *App) ListPluginProviderUsage(ctx context.Context, pluginID string, limit int) ([]storage.PluginProviderUsage, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Plugins.ListPluginProviderUsage(ctx, pluginID, limit)
}

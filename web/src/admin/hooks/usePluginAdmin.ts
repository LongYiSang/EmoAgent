import { useCallback, useMemo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import {
  deletePlugin,
  disablePlugin,
  enablePlugin,
  installGitHubPlugin,
  installLocalPlugin,
  loadPlugin,
  loadPluginAccessEvents,
  loadPluginLogs,
  loadPluginProviderUsage,
  loadPlugins,
  restartPlugin,
  type PluginSummary,
} from '../protocol/pluginApi';
import type { AdminStatusControls } from './useAdminStatus';

type PluginAdminOptions = Pick<AdminStatusControls, 'showError' | 'setStatus'>;

export function usePluginAdmin({ showError, setStatus }: PluginAdminOptions) {
  const [plugins, setPlugins] = useState<PluginSummary[]>([]);
  const [selectedPluginID, setSelectedPluginID] = useState('');
  const [selectedPlugin, setSelectedPlugin] = useState<PluginSummary | null>(null);
  const [installPath, setInstallPath] = useState('');
  const [githubOwner, setGithubOwner] = useState('');
  const [githubRepo, setGithubRepo] = useState('');
  const [githubTag, setGithubTag] = useState('');
  const [githubAsset, setGithubAsset] = useState('');
  const [grantJSON, setGrantJSON] = useState('{}');
  const [pluginLogs, setPluginLogs] = useState('');
  const [accessEvents, setAccessEvents] = useState<AnyRecord[]>([]);
  const [providerUsage, setProviderUsage] = useState<AnyRecord[]>([]);

  const reloadPlugins = useCallback(async () => {
    const next = await loadPlugins();
    setPlugins(next);
    if (selectedPluginID) {
      const current = next.find(item => item.plugin_id === selectedPluginID) || null;
      setSelectedPlugin(current);
    }
  }, [selectedPluginID]);

  const reloadPluginDetail = useCallback(async (id = selectedPluginID) => {
    if (!id) return;
    const [detail, logs, events, usage] = await Promise.all([
      loadPlugin(id),
      loadPluginLogs(id),
      loadPluginAccessEvents(id),
      loadPluginProviderUsage(id),
    ]);
    setSelectedPlugin(detail);
    setPluginLogs(logs);
    setAccessEvents(events);
    setProviderUsage(usage);
  }, [selectedPluginID]);

  const selectPlugin = useCallback(async (id: string) => {
    setSelectedPluginID(id);
    try {
      await reloadPluginDetail(id);
    } catch (error) {
      showError(error);
    }
  }, [reloadPluginDetail, showError]);

  const installLocal = useCallback(async () => {
    try {
      const installed = await installLocalPlugin(installPath);
      setSelectedPluginID(installed.plugin_id);
      setSelectedPlugin(installed);
      setStatus(`已安装 ${installed.plugin_id}`);
      await reloadPlugins();
      await reloadPluginDetail(installed.plugin_id);
    } catch (error) {
      showError(error);
    }
  }, [installPath, reloadPluginDetail, reloadPlugins, setStatus, showError]);

  const installGitHub = useCallback(async () => {
    try {
      const installed = await installGitHubPlugin(githubOwner, githubRepo, githubTag, githubAsset);
      setSelectedPluginID(installed.plugin_id);
      setSelectedPlugin(installed);
      setStatus(`已安装 ${installed.plugin_id}`);
      await reloadPlugins();
      await reloadPluginDetail(installed.plugin_id);
    } catch (error) {
      showError(error);
    }
  }, [githubAsset, githubOwner, githubRepo, githubTag, reloadPluginDetail, reloadPlugins, setStatus, showError]);

  const enableSelectedPlugin = useCallback(async () => {
    if (!selectedPluginID) return;
    try {
      const summary = await enablePlugin(selectedPluginID, grantJSON);
      setSelectedPlugin(summary);
      setStatus(`已启用 ${selectedPluginID}`);
      await reloadPlugins();
      await reloadPluginDetail(selectedPluginID);
    } catch (error) {
      showError(error);
    }
  }, [grantJSON, reloadPluginDetail, reloadPlugins, selectedPluginID, setStatus, showError]);

  const disableSelectedPlugin = useCallback(async () => {
    if (!selectedPluginID) return;
    try {
      const summary = await disablePlugin(selectedPluginID);
      setSelectedPlugin(summary);
      setStatus(`已禁用 ${selectedPluginID}`);
      await reloadPlugins();
      await reloadPluginDetail(selectedPluginID);
    } catch (error) {
      showError(error);
    }
  }, [reloadPluginDetail, reloadPlugins, selectedPluginID, setStatus, showError]);

  const restartSelectedPlugin = useCallback(async () => {
    if (!selectedPluginID) return;
    try {
      const summary = await restartPlugin(selectedPluginID);
      setSelectedPlugin(summary);
      setStatus(`已重启 ${selectedPluginID}`);
      await reloadPlugins();
      await reloadPluginDetail(selectedPluginID);
    } catch (error) {
      showError(error);
    }
  }, [reloadPluginDetail, reloadPlugins, selectedPluginID, setStatus, showError]);

  const deleteSelectedPlugin = useCallback(async () => {
    if (!selectedPluginID) return;
    try {
      await deletePlugin(selectedPluginID);
      setSelectedPluginID('');
      setSelectedPlugin(null);
      setPluginLogs('');
      setAccessEvents([]);
      setProviderUsage([]);
      setStatus(`已删除 ${selectedPluginID}`);
      await reloadPlugins();
    } catch (error) {
      showError(error);
    }
  }, [reloadPlugins, selectedPluginID, setStatus, showError]);

  return useMemo(() => ({
    plugins,
    selectedPluginID,
    selectedPlugin,
    installPath,
    githubOwner,
    githubRepo,
    githubTag,
    githubAsset,
    grantJSON,
    pluginLogs,
    accessEvents,
    providerUsage,
    setInstallPath,
    setGithubOwner,
    setGithubRepo,
    setGithubTag,
    setGithubAsset,
    setGrantJSON,
    reloadPlugins,
    reloadPluginDetail,
    selectPlugin,
    installLocal,
    installGitHub,
    enableSelectedPlugin,
    disableSelectedPlugin,
    restartSelectedPlugin,
    deleteSelectedPlugin,
  }), [
    plugins,
    selectedPluginID,
    selectedPlugin,
    installPath,
    githubOwner,
    githubRepo,
    githubTag,
    githubAsset,
    grantJSON,
    pluginLogs,
    accessEvents,
    providerUsage,
    reloadPlugins,
    reloadPluginDetail,
    selectPlugin,
    installLocal,
    installGitHub,
    enableSelectedPlugin,
    disableSelectedPlugin,
    restartSelectedPlugin,
    deleteSelectedPlugin,
  ]);
}

export type PluginAdmin = ReturnType<typeof usePluginAdmin>;

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
  const [selectedPluginVersion, setSelectedPluginVersion] = useState('');
  const [selectedPlugin, setSelectedPlugin] = useState<PluginSummary | null>(null);
  const [installPath, setInstallPath] = useState('');
  const [githubOwner, setGithubOwner] = useState('');
  const [githubRepo, setGithubRepo] = useState('');
  const [githubTag, setGithubTag] = useState('');
  const [githubAsset, setGithubAsset] = useState('');
  const [grantJSON, setGrantJSON] = useState('{}');
  const [trustReviewGrantJSON, setTrustReviewGrantJSON] = useState('');
  const [pluginLogs, setPluginLogs] = useState('');
  const [accessEvents, setAccessEvents] = useState<AnyRecord[]>([]);
  const [providerUsage, setProviderUsage] = useState<AnyRecord[]>([]);

  const reloadPlugins = useCallback(async () => {
    const next = await loadPlugins();
    setPlugins(next);
    if (selectedPluginID) {
      const current =
        next.find(item => item.plugin_id === selectedPluginID && item.version === selectedPluginVersion) ||
        next.find(item => item.plugin_id === selectedPluginID && item.enabled) ||
        next.find(item => item.plugin_id === selectedPluginID) ||
        null;
      setSelectedPlugin(current);
      setSelectedPluginVersion(current?.version || selectedPluginVersion);
    }
  }, [selectedPluginID, selectedPluginVersion]);

  const reloadPluginDetail = useCallback(async (id = selectedPluginID, version = selectedPluginVersion, userGrantJSON = '') => {
    if (!id) return;
    const [detail, logs, events, usage] = await Promise.all([
      loadPlugin(id, version, userGrantJSON),
      loadPluginLogs(id),
      loadPluginAccessEvents(id),
      loadPluginProviderUsage(id),
    ]);
    setSelectedPlugin(detail);
    setSelectedPluginVersion(detail.version || version);
    setTrustReviewGrantJSON(userGrantJSON);
    setPluginLogs(logs);
    setAccessEvents(events);
    setProviderUsage(usage);
  }, [selectedPluginID, selectedPluginVersion]);

  const selectPlugin = useCallback(async (id: string, version?: string) => {
    setSelectedPluginID(id);
    setSelectedPluginVersion(version || '');
    try {
      await reloadPluginDetail(id, version || '');
    } catch (error) {
      showError(error);
    }
  }, [reloadPluginDetail, showError]);

  const installLocal = useCallback(async () => {
    try {
      const installed = await installLocalPlugin(installPath);
      setSelectedPluginID(installed.plugin_id);
      setSelectedPluginVersion(installed.version);
      setSelectedPlugin(installed);
      setStatus(`已安装 ${installed.plugin_id}`);
      await reloadPlugins();
      await reloadPluginDetail(installed.plugin_id, installed.version);
    } catch (error) {
      showError(error);
    }
  }, [installPath, reloadPluginDetail, reloadPlugins, setStatus, showError]);

  const installGitHub = useCallback(async () => {
    try {
      const installed = await installGitHubPlugin(githubOwner, githubRepo, githubTag, githubAsset);
      setSelectedPluginID(installed.plugin_id);
      setSelectedPluginVersion(installed.version);
      setSelectedPlugin(installed);
      setStatus(`已安装 ${installed.plugin_id}`);
      await reloadPlugins();
      await reloadPluginDetail(installed.plugin_id, installed.version);
    } catch (error) {
      showError(error);
    }
  }, [githubAsset, githubOwner, githubRepo, githubTag, reloadPluginDetail, reloadPlugins, setStatus, showError]);

  const enableSelectedPlugin = useCallback(async () => {
    if (!selectedPluginID) return;
    try {
      const version = selectedPlugin?.version || selectedPluginVersion;
      let trustAcknowledgement = selectedPlugin?.trust_review?.required && trustReviewGrantJSON === grantJSON ? selectedPlugin.trust_review.acknowledgement : undefined;
      if (!trustAcknowledgement) {
        const preview = await loadPlugin(selectedPluginID, version, grantJSON);
        setSelectedPlugin(preview);
        setSelectedPluginVersion(preview.version || version);
        setTrustReviewGrantJSON(grantJSON);
        if (preview.trust_review?.required) {
          setStatus('请确认本次插件信任/策略变更后再次点击启用');
          return;
        }
      }
      const summary = await enablePlugin(selectedPluginID, grantJSON, version, trustAcknowledgement);
      setSelectedPlugin(summary);
      setSelectedPluginVersion(summary.version || version);
      setTrustReviewGrantJSON('');
      setStatus(`已启用 ${selectedPluginID}@${summary.version || version}`);
      await reloadPlugins();
      await reloadPluginDetail(selectedPluginID, summary.version || version);
    } catch (error) {
      if (String(error).includes('acknowledgement')) {
        await reloadPluginDetail(selectedPluginID, selectedPlugin?.version || selectedPluginVersion, grantJSON);
      }
      showError(error);
    }
  }, [grantJSON, reloadPluginDetail, reloadPlugins, selectedPlugin?.trust_review?.acknowledgement, selectedPlugin?.trust_review?.required, selectedPlugin?.version, selectedPluginID, selectedPluginVersion, setStatus, showError, trustReviewGrantJSON]);

  const disableSelectedPlugin = useCallback(async () => {
    if (!selectedPluginID) return;
    try {
      const summary = await disablePlugin(selectedPluginID);
      setSelectedPlugin(summary);
      setSelectedPluginVersion(summary.version || selectedPluginVersion);
      setStatus(`已禁用 ${selectedPluginID}`);
      await reloadPlugins();
      await reloadPluginDetail(selectedPluginID, summary.version || selectedPluginVersion);
    } catch (error) {
      showError(error);
    }
  }, [reloadPluginDetail, reloadPlugins, selectedPluginID, selectedPluginVersion, setStatus, showError]);

  const restartSelectedPlugin = useCallback(async () => {
    if (!selectedPluginID) return;
    try {
      const summary = await restartPlugin(selectedPluginID);
      setSelectedPlugin(summary);
      setSelectedPluginVersion(summary.version || selectedPluginVersion);
      setStatus(`已重启 ${selectedPluginID}`);
      await reloadPlugins();
      await reloadPluginDetail(selectedPluginID, summary.version || selectedPluginVersion);
    } catch (error) {
      showError(error);
    }
  }, [reloadPluginDetail, reloadPlugins, selectedPluginID, selectedPluginVersion, setStatus, showError]);

  const deleteSelectedPlugin = useCallback(async () => {
    if (!selectedPluginID) return;
    try {
      await deletePlugin(selectedPluginID);
      setSelectedPluginID('');
      setSelectedPluginVersion('');
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
    selectedPluginVersion,
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
    selectedPluginVersion,
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

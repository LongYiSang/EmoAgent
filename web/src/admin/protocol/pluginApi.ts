import { requestJSON, type AnyRecord } from '../../shared/lib/api';

export type PluginRuntimeStatus = {
  plugin_id?: string;
  status?: string;
  last_error?: string;
  restart_count?: number;
  stderr_tail?: string;
};

export type PluginSummary = {
  plugin_id: string;
  version: string;
  name: string;
  runtime_kind?: string;
  access_tier?: string;
  capabilities?: string[];
  hooks?: AnyRecord[];
  enabled?: boolean;
  runtime_status?: PluginRuntimeStatus;
  package_digest?: string;
  manifest_digest?: string;
  signature_status?: string;
  publisher_id?: string;
  source_type?: string;
  source_ref?: string;
  installed_at?: string;
  store_path?: string;
  state_path?: string;
  cache_path?: string;
  run_path?: string;
  workspace_path?: string;
  provider_usage_today?: AnyRecord;
};

export async function loadPlugins(): Promise<PluginSummary[]> {
  const data = await requestJSON<{ plugins?: PluginSummary[] }>('/api/plugins');
  return data.plugins || [];
}

export async function loadPlugin(id: string): Promise<PluginSummary> {
  return requestJSON<PluginSummary>(`/api/plugins/${encodeURIComponent(id)}`);
}

export async function installLocalPlugin(path: string): Promise<PluginSummary> {
  return requestJSON<PluginSummary>('/api/plugins/install/local', { method: 'POST', body: { path } });
}

export async function installGitHubPlugin(owner: string, repo: string, tag: string, asset: string): Promise<PluginSummary> {
  return requestJSON<PluginSummary>('/api/plugins/install/github-release', { method: 'POST', body: { owner, repo, tag, asset } });
}

export async function enablePlugin(id: string, userGrantJSON: string): Promise<PluginSummary> {
  return requestJSON<PluginSummary>(`/api/plugins/${encodeURIComponent(id)}/enable`, {
    method: 'POST',
    body: { user_grant_json: userGrantJSON || '{}' },
  });
}

export async function disablePlugin(id: string): Promise<PluginSummary> {
  return requestJSON<PluginSummary>(`/api/plugins/${encodeURIComponent(id)}/disable`, { method: 'POST' });
}

export async function restartPlugin(id: string): Promise<PluginSummary> {
  return requestJSON<PluginSummary>(`/api/plugins/${encodeURIComponent(id)}/restart`, { method: 'POST' });
}

export async function deletePlugin(id: string): Promise<void> {
  await requestJSON(`/api/plugins/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export async function loadPluginStatus(id: string): Promise<PluginRuntimeStatus> {
  return requestJSON<PluginRuntimeStatus>(`/api/plugins/${encodeURIComponent(id)}/status`);
}

export async function loadPluginLogs(id: string): Promise<string> {
  const data = await requestJSON<{ stderr_tail?: string }>(`/api/plugins/${encodeURIComponent(id)}/logs`);
  return data.stderr_tail || '';
}

export async function loadPluginAccessEvents(id: string): Promise<AnyRecord[]> {
  const data = await requestJSON<{ events?: AnyRecord[] }>(`/api/plugins/${encodeURIComponent(id)}/access-events?limit=25`);
  return data.events || [];
}

export async function loadPluginProviderUsage(id: string): Promise<AnyRecord[]> {
  const data = await requestJSON<{ usage?: AnyRecord[] }>(`/api/plugins/${encodeURIComponent(id)}/provider-usage?limit=25`);
  return data.usage || [];
}

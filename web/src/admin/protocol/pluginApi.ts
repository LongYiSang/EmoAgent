import { requestJSON, type AnyRecord } from '../../shared/lib/api';

export type PluginRuntimeStatus = {
  plugin_id?: string;
  runtime_kind?: string;
  status?: string;
  last_error?: string;
  restart_count?: number;
  stderr_tail?: string;
  python_executable_path?: string;
  python_executable_source?: string;
  python_executable_available?: boolean;
  dependency_env_dir?: string;
  pid?: number;
  process_guard_kind?: string;
  process_guard_attached?: boolean;
  process_guard_error?: string;
};

export type PluginTrustAcknowledgement = {
  plugin_id?: string;
  version?: string;
  package_digest?: string;
  manifest_digest?: string;
  signature_status?: string;
  publisher_id?: string;
  default_tool_exposure?: string;
  default_invocation_policy?: string;
  target_user_grant_hash?: string;
  dependency_lock_digest?: string;
  ack_nonce?: string;
  ack_issued_at?: string;
  user_action?: string;
  reasons?: string[];
};

export type PluginTrustReview = {
  required?: boolean;
  reasons?: string[];
  acknowledgement?: PluginTrustAcknowledgement;
};

export type PluginTrustAcceptance = {
  trust_level?: string;
  accepted_at?: string;
  acknowledgement_hash?: string;
  reasons?: string[];
  default_tool_exposure?: string;
  default_invocation_policy?: string;
};

export type PluginDependencyPackageSummary = {
  name?: string;
  kind?: string;
  path?: string;
  sha256?: string;
};

export type PluginDependencySummary = {
  present?: boolean;
  lock_digest?: string;
  package_count?: number;
  packages?: PluginDependencyPackageSummary[];
};

export type PluginHostAPIPolicy = {
  manifest_capabilities?: string[];
  user_granted_capabilities?: string[];
  host_allowed_capabilities?: string[];
  effective_capabilities?: string[];
  host_policy_mode?: string;
};

export type PluginToolPolicyEntry = {
  name?: string;
  host_exposure?: string;
  host_invocation?: string;
  self_reported_scope?: string;
  self_reported_permission?: string;
};

export type PluginToolPolicy = {
  default_exposure?: string;
  default_invocation?: string;
  registered_tools?: PluginToolPolicyEntry[];
};

export type PluginHookPolicy = {
  allow_active_hooks?: boolean;
  observe_hooks?: string[];
  active_hooks?: string[];
};

export type PluginSettingsFieldSchema = {
  type?: 'string' | 'number' | 'integer' | 'boolean' | string;
  title?: string;
  description?: string;
  enum?: string[];
  enum_titles?: Record<string, string>;
  secret?: boolean;
  default?: unknown;
};

export type PluginSettingsSchema = {
  type?: 'object' | string;
  required?: string[];
  properties?: Record<string, PluginSettingsFieldSchema>;
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
  trust_level?: string;
  trust_acceptance?: PluginTrustAcceptance;
  trust_review?: PluginTrustReview;
  dependency_summary?: PluginDependencySummary;
  host_api_policy?: PluginHostAPIPolicy;
  tool_policy?: PluginToolPolicy;
  hook_policy?: PluginHookPolicy;
  settings_schema?: PluginSettingsSchema;
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

export async function loadPlugin(id: string, version?: string, userGrantJSON?: string): Promise<PluginSummary> {
	const query = new URLSearchParams();
	if (version) query.set('version', version);
	if (userGrantJSON) query.set('user_grant_json', userGrantJSON);
	const suffix = query.toString() ? `?${query.toString()}` : '';
	return requestJSON<PluginSummary>(`/api/plugins/${encodeURIComponent(id)}${suffix}`);
}

export type PluginSettings = {
	plugin_id?: string;
	key?: string;
	found?: boolean;
	value?: AnyRecord;
};

export async function loadPluginSettings(id: string): Promise<PluginSettings> {
	return requestJSON<PluginSettings>(`/api/plugins/${encodeURIComponent(id)}/settings`);
}

export async function updatePluginSettings(id: string, value: AnyRecord): Promise<PluginSettings> {
	return requestJSON<PluginSettings>(`/api/plugins/${encodeURIComponent(id)}/settings`, {
		method: 'PUT',
		body: { value },
	});
}

export async function installLocalPlugin(path: string): Promise<PluginSummary> {
	return requestJSON<PluginSummary>('/api/plugins/install/local', { method: 'POST', body: { path } });
}

export async function installGitHubPlugin(owner: string, repo: string, tag: string, asset: string): Promise<PluginSummary> {
  return requestJSON<PluginSummary>('/api/plugins/install/github-release', { method: 'POST', body: { owner, repo, tag, asset } });
}

export async function enablePlugin(id: string, userGrantJSON: string, version?: string, trustAcknowledgement?: PluginTrustAcknowledgement): Promise<PluginSummary> {
  return requestJSON<PluginSummary>(`/api/plugins/${encodeURIComponent(id)}/enable`, {
    method: 'POST',
    body: {
      user_grant_json: userGrantJSON || '{}',
      ...(version ? { version } : {}),
      ...(trustAcknowledgement ? { trust_acknowledgement: trustAcknowledgement } : {}),
    },
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

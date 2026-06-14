import { requestJSON } from '../../shared/lib/api';

export type PromptOverride = {
  id?: string;
  component_id: string;
  scope_type: 'global' | 'agent';
  scope_id?: string;
  mode: 'custom' | 'use_default';
  override_text: string;
  enabled: boolean;
  default_hash_at_edit?: string;
  note?: string;
  created_at?: string;
  updated_at?: string;
};

export type PromptComponentDetail = {
  id: string;
  group: string;
  name: string;
  description: string;
  kind: string;
  default_text: string;
  editable: boolean;
  risk_level: string;
  scope_support: string[];
  max_chars: number;
  order: number;
  global_override?: PromptOverride;
  agent_override?: PromptOverride;
  effective_text: string;
  effective_source: string;
  effective_scope_type: string;
  effective_scope_id: string;
  default_hash: string;
  effective_hash: string;
  default_hash_at_edit?: string;
  stale_override: boolean;
};

export type PromptComponentsResponse = {
  agent_id: string;
  components: PromptComponentDetail[];
};

export type PromptPreviewResponse = {
  agent_id: string;
  persona_key: string;
  purpose: string;
  rendered_text: string;
  final_hash: string;
  components: Array<{
    component_id: string;
    source: string;
    effective_hash: string;
  }>;
};

export type PromptSnapshotSummary = {
  id: string;
  session_id: string;
  agent_id: string;
  persona_key: string;
  purpose: string;
  model: string;
  final_hash: string;
  truncated: boolean;
  created_at: string;
};

export type PromptSnapshotDetail = PromptSnapshotSummary & {
  request_id: string;
  turn_id: string;
  components: Array<Record<string, unknown>>;
  components_json: string;
  rendered_text: string;
};

export async function loadPromptComponents(agentID: string): Promise<PromptComponentsResponse> {
  const query = agentID ? `?agent_id=${encodeURIComponent(agentID)}` : '';
  return requestJSON<PromptComponentsResponse>(`/api/prompts/components${query}`);
}

export async function loadPromptComponent(componentID: string, agentID: string): Promise<PromptComponentDetail> {
  const query = agentID ? `?agent_id=${encodeURIComponent(agentID)}` : '';
  return requestJSON<PromptComponentDetail>(`/api/prompts/components/${encodeURIComponent(componentID)}${query}`);
}

export async function savePromptOverride(req: {
  component_id: string;
  scope_type: 'global' | 'agent';
  scope_id?: string;
  mode: 'custom' | 'use_default';
  override_text?: string;
  note?: string;
}): Promise<void> {
  await requestJSON('/api/prompts/overrides', {
    method: 'PUT',
    body: {
      ...req,
      scope_id: req.scope_id || '',
      override_text: req.override_text || '',
      enabled: true,
    },
  });
}

export async function deletePromptOverride(componentID: string, scopeType: 'global' | 'agent', scopeID = ''): Promise<void> {
  const params = new URLSearchParams({ component_id: componentID, scope_type: scopeType, scope_id: scopeID });
  await requestJSON(`/api/prompts/overrides?${params.toString()}`, { method: 'DELETE' });
}

export async function previewPrompt(req: {
  agent_id: string;
  purpose: string;
  component_ids: string[];
}): Promise<PromptPreviewResponse> {
  return requestJSON<PromptPreviewResponse>('/api/prompts/preview', { method: 'POST', body: req });
}

export async function loadPromptSnapshots(query: {
  agent_id?: string;
  session_id?: string;
  purpose?: string;
  limit?: number;
}): Promise<PromptSnapshotSummary[]> {
  const params = new URLSearchParams();
  if (query.agent_id) params.set('agent_id', query.agent_id);
  if (query.session_id) params.set('session_id', query.session_id);
  if (query.purpose) params.set('purpose', query.purpose);
  if (query.limit) params.set('limit', String(query.limit));
  const suffix = params.toString() ? `?${params.toString()}` : '';
  const data = await requestJSON<{ snapshots?: PromptSnapshotSummary[] }>(`/api/prompts/snapshots${suffix}`);
  return data.snapshots || [];
}

export async function loadPromptSnapshot(id: string): Promise<PromptSnapshotDetail> {
  return requestJSON<PromptSnapshotDetail>(`/api/prompts/snapshots/${encodeURIComponent(id)}`);
}

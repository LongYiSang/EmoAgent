import { requestJSON, type AnyRecord } from '../../shared/lib/api';

export type ProviderPreset = AnyRecord & { id?: string; name?: string; provider_capabilities?: string[] };
export type Provider = AnyRecord & { id?: string; name?: string; capabilities?: string[] };
export type AgentConfig = AnyRecord & { id?: string; name?: string; persona_key?: string };
export type Persona = AnyRecord & { key?: string; name?: string };

export async function loadProviderPresets(): Promise<ProviderPreset[]> {
  const data = await requestJSON<{ presets?: ProviderPreset[] }>('/api/llm-provider-presets');
  return data.presets || [];
}

export async function loadProviders(): Promise<Provider[]> {
  const data = await requestJSON<{ providers?: Provider[] }>('/api/llm-providers');
  return data.providers || [];
}

export async function loadProviderModels(id: string): Promise<AnyRecord[]> {
  const data = await requestJSON<{ models?: AnyRecord[] }>(`/api/llm-providers/${encodeURIComponent(id)}/models`);
  return data.models || [];
}

export async function loadProviderEnvStatus(id: string): Promise<AnyRecord> {
  return requestJSON<AnyRecord>(`/api/llm-providers/${encodeURIComponent(id)}/env-status`);
}

export async function testProvider(id: string): Promise<AnyRecord> {
  return requestJSON<AnyRecord>(`/api/providers/${encodeURIComponent(id)}/test`, { method: 'POST' });
}

export async function saveProvider(provider: Provider, editingID: string): Promise<void> {
  await requestJSON(editingID ? `/api/llm-providers/${encodeURIComponent(editingID)}` : '/api/llm-providers', {
    method: editingID ? 'PUT' : 'POST',
    body: provider,
  });
}

export async function deleteProvider(id: string): Promise<void> {
  await requestJSON(`/api/llm-providers/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export async function refreshProviderModels(id: string): Promise<AnyRecord[]> {
  const data = await requestJSON<{ models?: AnyRecord[] }>(`/api/llm-providers/${encodeURIComponent(id)}/refresh-models`, { method: 'POST' });
  return data.models || [];
}

export async function loadAgents(): Promise<{ configs: AgentConfig[]; activeID: string }> {
  const data = await requestJSON<{ configs?: AgentConfig[]; active_id?: string }>('/api/agent-configs');
  return { configs: data.configs || [], activeID: data.active_id || '' };
}

export async function saveAgent(agent: AgentConfig, editingID: string): Promise<void> {
  await requestJSON(editingID ? `/api/agent-configs/${encodeURIComponent(editingID)}` : '/api/agent-configs', {
    method: editingID ? 'PUT' : 'POST',
    body: agent,
  });
}

export async function activateAgent(id: string): Promise<void> {
  await requestJSON(`/api/agent-configs/${encodeURIComponent(id)}/activate`, { method: 'POST' });
}

export async function deleteAgent(id: string): Promise<void> {
  await requestJSON(`/api/agent-configs/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export async function loadChatSettings(): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/settings/chat');
}

export async function saveChatSettings(settings: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/settings/chat', { method: 'PUT', body: settings });
}

export async function loadPersonas(): Promise<Persona[]> {
  const data = await requestJSON<{ personas?: Persona[] }>('/api/personas');
  return data.personas || [];
}

export async function loadPersona(key: string): Promise<Persona> {
  return requestJSON<Persona>(`/api/personas/${encodeURIComponent(key)}`);
}

export async function savePersona(persona: Persona, editingKey: string): Promise<void> {
  await requestJSON(editingKey ? `/api/personas/${encodeURIComponent(editingKey)}` : '/api/personas', {
    method: editingKey ? 'PUT' : 'POST',
    body: persona,
  });
}

export async function deletePersona(key: string): Promise<void> {
  await requestJSON(`/api/personas/${encodeURIComponent(key)}`, { method: 'DELETE' });
}

export async function loadProgressPhrases(key: string): Promise<AnyRecord> {
  const data = await requestJSON<{ phrases?: AnyRecord }>(`/api/personas/${encodeURIComponent(key)}/progress-phrases`);
  return data.phrases || {};
}

export async function saveProgressPhrases(key: string, phrases: AnyRecord): Promise<void> {
  await requestJSON(`/api/personas/${encodeURIComponent(key)}/progress-phrases`, { method: 'PUT', body: { phrases } });
}

export async function loadDefaultProgressPhrases(): Promise<AnyRecord> {
  const data = await requestJSON<{ phrases?: AnyRecord }>('/api/progress-phrases/defaults');
  return data.phrases || {};
}

export async function loadEffectiveConfig(): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/config/effective');
}

export async function validateConfig(): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/config/validate', { method: 'POST', body: {} });
}

export async function loadConfigIssues(): Promise<AnyRecord[]> {
  const data = await requestJSON<{ issues?: AnyRecord[] }>('/api/config/issues');
  return data.issues || [];
}

export async function saveMemoryConfig(memory: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/memory/config', { method: 'PUT', body: { memory } });
}

export async function loadMemoryFeatures(): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/memory/features');
}

export async function saveMemoryFeatures(memory: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/memory/features', { method: 'PUT', body: { memory } });
}

export async function loadMemoryExtractions(): Promise<AnyRecord[]> {
  const data = await requestJSON<{ jobs?: AnyRecord[] }>('/api/memory/extractions?limit=20');
  return data.jobs || [];
}

export async function loadMemorySegments(): Promise<AnyRecord[]> {
  const data = await requestJSON<{ segments?: AnyRecord[] }>('/api/memory/segments');
  return data.segments || [];
}

export async function loadSidecarStatus(): Promise<{ status: AnyRecord; generated: string; logs: string }> {
  const [status, generated, logs] = await Promise.all([
    requestJSON<AnyRecord>('/api/sidecar/status'),
    requestJSON<{ config?: string }>('/api/sidecar/generated-config'),
    requestJSON<{ logs?: string }>('/api/sidecar/logs?max_bytes=20000'),
  ]);
  return { status, generated: generated.config || '', logs: logs.logs || '' };
}

export async function sidecarAction(action: 'start' | 'stop' | 'restart'): Promise<AnyRecord> {
  return requestJSON<AnyRecord>(`/api/sidecar/${action}`, { method: 'POST' });
}

export async function loadNaturalMemoryLatest(): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/memory/natural-runs/latest');
}

export async function runNaturalMemory(dryRun: boolean): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/memory/natural-runs', { method: 'POST', body: { mode: 'manual', dry_run: dryRun, explain: true } });
}

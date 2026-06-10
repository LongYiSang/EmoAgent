import { requestJSON, type AnyRecord } from '../../shared/lib/api';

function params(query: Record<string, string | number | undefined>) {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== '') search.set(key, String(value));
  }
  const body = search.toString();
  return body ? `?${body}` : '';
}

export async function loadAgentAffectConfig(): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/config');
}

export async function saveAgentAffectConfig(agentAffect: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/config', {
    method: 'PUT',
    body: { agent_affect: agentAffect },
  });
}

export async function loadAgentAffectCurrent(query: { personaID: string; sessionID: string }): Promise<AnyRecord> {
  return requestJSON<AnyRecord>(`/api/agent-affect/current${params({ persona_id: query.personaID, session_id: query.sessionID })}`);
}

export async function loadAgentAffectHistory(query: { personaID: string; sessionID: string; kind?: string; limit?: number }): Promise<AnyRecord> {
  return requestJSON<AnyRecord>(`/api/agent-affect/history${params({ persona_id: query.personaID, session_id: query.sessionID, kind: query.kind, limit: query.limit })}`);
}

export async function loadAgentAffectPluginWrites(query: { personaID?: string; sessionID?: string; pluginID?: string; limit?: number }): Promise<AnyRecord[]> {
  const data = await requestJSON<{ writes?: AnyRecord[] }>(`/api/agent-affect/plugin-writes${params({ persona_id: query.personaID, session_id: query.sessionID, plugin_id: query.pluginID, limit: query.limit })}`);
  return data.writes || [];
}

export async function loadAgentAffectQueue(query: { personaID?: string; sessionID?: string; limit?: number }): Promise<AnyRecord> {
  return requestJSON<AnyRecord>(`/api/agent-affect/queue${params({ persona_id: query.personaID, session_id: query.sessionID, limit: query.limit })}`);
}

export async function loadAgentAffectProfile(personaID: string): Promise<AnyRecord> {
  return requestJSON<AnyRecord>(`/api/agent-affect/profile${params({ persona_id: personaID })}`);
}

export async function saveAgentAffectProfile(profile: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/profile', { method: 'PUT', body: profile });
}

export async function previewAgentAffectPrompt(req: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/prompt-preview', { method: 'POST', body: req });
}

export async function evaluateAgentAffect(req: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/evaluate', { method: 'POST', body: req });
}

export async function submitAgentAffect(req: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/submit', { method: 'POST', body: req });
}

export async function applyAgentAffectDelta(req: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/delta', { method: 'POST', body: req });
}

export async function resetAgentAffect(req: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/reset', { method: 'POST', body: req });
}

export async function processAgentAffectOnce(): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/process-once', { method: 'POST' });
}

export async function clearAgentAffectFailedJobs(req: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/clear-failed', { method: 'POST', body: req });
}

export async function supersedeAgentAffectPendingJobs(req: AnyRecord): Promise<AnyRecord> {
  return requestJSON<AnyRecord>('/api/agent-affect/supersede-pending', { method: 'POST', body: req });
}

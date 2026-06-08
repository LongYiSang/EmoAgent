import { requestJSON } from '../../shared/lib/api';
import type { ApprovalRequest } from './wsTypes';

export type MessageRecord = {
  id?: string;
  ID?: string;
  role?: string;
  Role?: string;
  content?: string;
  Content?: string;
  created_at?: string;
  createdAt?: string;
  CreatedAt?: string;
  metadata?: unknown;
  Metadata?: unknown;
};

export type SessionSummary = {
  id?: string;
  ID?: string;
  title?: string;
  Title?: string;
  persona?: string;
  Persona?: string;
  updated_at?: string;
  updatedAt?: string;
  UpdatedAt?: string;
  last_message?: string;
  lastMessage?: string;
  LastMessage?: string;
  message_count?: number;
  MessageCount?: number;
};

export type SessionDetail = {
  id?: string;
  ID?: string;
  persona?: string;
  Persona?: string;
  messages?: MessageRecord[];
  Messages?: MessageRecord[];
};

export async function loadSessions(persona: string): Promise<SessionSummary[]> {
  const data = await requestJSON<{ sessions?: SessionSummary[] }>(`/api/sessions?persona=${encodeURIComponent(persona)}&limit=20`);
  return data.sessions || [];
}

export async function loadSessionDetail(id: string): Promise<SessionDetail> {
  return requestJSON<SessionDetail>(`/api/sessions/${encodeURIComponent(id)}`);
}

export async function deleteSession(id: string): Promise<void> {
  await requestJSON(`/api/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export async function loadSessionApprovals(id: string): Promise<ApprovalRequest[]> {
  const data = await requestJSON<{ approvals?: ApprovalRequest[] }>(`/api/sessions/${encodeURIComponent(id)}/approvals`);
  return data.approvals || [];
}

export async function loadDefaultPersona(): Promise<string> {
  try {
    const active = await requestJSON<{ persona_key?: string }>('/api/agent-configs/active');
    if (active.persona_key) return active.persona_key;
  } catch {
    // Fall back to the persona list when no active agent is available.
  }
  const personas = await requestJSON<{ personas?: Array<{ key?: string }> }>('/api/personas');
  return personas.personas?.[0]?.key || '';
}

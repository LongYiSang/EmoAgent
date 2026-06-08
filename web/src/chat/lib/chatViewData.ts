import { field, stringField } from '../../shared/lib/data';

export function syncURL(personaKey: string, sessionID: string) {
  const url = new URL(location.href);
  if (personaKey) url.searchParams.set('persona', personaKey);
  else url.searchParams.delete('persona');
  if (sessionID) url.searchParams.set('session_id', sessionID);
  else url.searchParams.delete('session_id');
  history.replaceState({}, '', url);
}

export function sessionIDOf(item: unknown): string {
  return stringField(item, 'id') || stringField(item, 'ID');
}

export function sessionPersonaOf(item: unknown): string {
  return stringField(item, 'persona') || stringField(item, 'Persona');
}

export function previewText(item: unknown): string {
  return stringField(item, 'last_message') || stringField(item, 'lastMessage') || stringField(item, 'LastMessage') || '还没有消息';
}

export function memoryStatusOf(item: unknown): string {
  return stringField(item, 'extraction_status') || stringField(item, 'ExtractionStatus') || stringField(item, 'status') || stringField(item, 'Status') || 'never';
}

export function memorySegmentLabel(item: unknown): string {
  const index = field<number | string>(item, 'segment_index', field(item, 'SegmentIndex', ''));
  const id = sessionIDOf(item);
  return index ? `片段 ${index}` : id ? `片段 · ${String(id).slice(0, 8)}` : '片段';
}

export function formatReasoningDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const seconds = ms / 1000;
  return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)} 秒`;
}

export function toolStatusLabel(status: string): string {
  if (status === 'success' || status === 'done') return '完成';
  if (status === 'running') return '运行中';
  if (status === 'approval_required') return '待审批';
  if (status === 'error' || status === 'failed') return '失败';
  return status || '工具';
}

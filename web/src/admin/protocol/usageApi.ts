import { requestJSON, type AnyRecord } from '../../shared/lib/api';

export type UsageFilter = {
  provider_id?: string;
  model?: string;
  component?: string;
  operation?: string;
  usage_source?: string;
  status?: string;
  group_by?: string;
  limit?: string;
  from?: string;
  to?: string;
};

export type LLMUsageEvent = AnyRecord;
export type LLMUsageSummaryRow = AnyRecord;
export type TokenEstimatorCalibration = AnyRecord;

export async function loadUsageEvents(filter: UsageFilter): Promise<LLMUsageEvent[]> {
  const data = await requestJSON<{ events?: LLMUsageEvent[] }>(`/api/llm-usage/events${query(filter)}`);
  return data.events || [];
}

export async function loadUsageSummary(filter: UsageFilter): Promise<LLMUsageSummaryRow[]> {
  const data = await requestJSON<{ rows?: LLMUsageSummaryRow[] }>(`/api/llm-usage/summary${query(filter)}`);
  return data.rows || [];
}

export async function loadTokenEstimatorCalibrations(filter: UsageFilter): Promise<TokenEstimatorCalibration[]> {
  const data = await requestJSON<{ calibrations?: TokenEstimatorCalibration[] }>(`/api/token-estimator/calibrations${query(filter)}`);
  return data.calibrations || [];
}

export async function refreshTokenEstimatorCalibrations(filter: UsageFilter): Promise<number> {
  const data = await requestJSON<{ refreshed?: number }>(`/api/token-estimator/calibrations/refresh${query(filter)}`, { method: 'POST' });
  return Number(data.refreshed || 0);
}

function query(filter: UsageFilter) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filter)) {
    const text = String(value || '').trim();
    if (text) params.set(key, text);
  }
  const raw = params.toString();
  return raw ? `?${raw}` : '';
}

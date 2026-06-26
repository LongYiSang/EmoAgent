import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  loadTokenEstimatorCalibrations,
  loadUsageEvents,
  loadUsageSummary,
  refreshTokenEstimatorCalibrations,
  type LLMUsageEvent,
  type LLMUsageSummaryRow,
  type TokenEstimatorCalibration,
  type UsageFilter,
} from '../protocol/usageApi';
import type { AdminStatusControls } from './useAdminStatus';

type UsageAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function useUsageAdmin({ setStatus, showError }: UsageAdminOptions) {
  const [filter, setFilter] = useState<UsageFilter>({ group_by: 'provider,model,component', limit: '100' });
  const [events, setEvents] = useState<LLMUsageEvent[]>([]);
  const [summary, setSummary] = useState<LLMUsageSummaryRow[]>([]);
  const [calibrations, setCalibrations] = useState<TokenEstimatorCalibration[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const patchUsageFilter = useCallback((key: keyof UsageFilter, value: string) => {
    setFilter(current => ({ ...current, [key]: value }));
  }, []);

  const clearUsageFilter = useCallback(() => {
    setEvents([]);
    setSummary([]);
    setCalibrations([]);
    setFilter({ group_by: 'provider,model,component', limit: '100' });
  }, []);

  const runLoad = useCallback(async (currentFilter: UsageFilter) => {
    setIsLoading(true);
    setStatus('正在加载用量数据...');
    try {
      const [nextEvents, nextSummary, nextCalibrations] = await Promise.all([
        loadUsageEvents(currentFilter),
        loadUsageSummary(currentFilter),
        loadTokenEstimatorCalibrations(currentFilter),
      ]);
      setEvents(nextEvents);
      setSummary(nextSummary);
      setCalibrations(nextCalibrations);
      setStatus('就绪');
    } catch (error) {
      showError(error);
    } finally {
      setIsLoading(false);
    }
  }, [setStatus, showError]);

  const reloadUsageAdmin = useCallback(async () => {
    await runLoad(filter);
  }, [filter, runLoad]);

  // Debounced auto-reload when filter changes
  useEffect(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }
    debounceRef.current = setTimeout(() => {
      runLoad(filter).catch(() => {});
    }, 450);
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, [filter, runLoad]);

  const refreshCalibrations = useCallback(async () => {
    setStatus('正在刷新校准数据...');
    try {
      const refreshed = await refreshTokenEstimatorCalibrations(filter);
      const nextCalibrations = await loadTokenEstimatorCalibrations(filter);
      setCalibrations(nextCalibrations);
      setStatus(`校准已刷新：${refreshed}`);
    } catch (error) {
      showError(error);
    }
  }, [filter, setStatus, showError]);

  return useMemo(() => ({
    usageFilter: filter,
    usageEvents: events,
    usageSummary: summary,
    tokenEstimatorCalibrations: calibrations,
    isLoading,
    patchUsageFilter,
    reloadUsageAdmin,
    refreshCalibrations,
    clearUsageFilter,
  }), [
    filter,
    events,
    summary,
    calibrations,
    isLoading,
    patchUsageFilter,
    reloadUsageAdmin,
    refreshCalibrations,
    clearUsageFilter,
  ]);
}

export type UsageAdmin = ReturnType<typeof useUsageAdmin>;

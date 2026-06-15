import { useCallback, useMemo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { field, pretty } from '../../shared/lib/data';
import { cloneRecord, setNestedValue } from '../lib/adminData';
import { loadWebSearchConfig, saveWebSearchConfig } from '../protocol/adminApi';
import type { AdminStatusControls } from './useAdminStatus';

type WebSearchAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function useWebSearchAdmin({ setStatus, showError }: WebSearchAdminOptions) {
  const [webSearchDraft, setWebSearchDraft] = useState<AnyRecord>({});
  const [webSearchRuntime, setWebSearchRuntime] = useState<AnyRecord>({});
  const [webSearchIssues, setWebSearchIssues] = useState<AnyRecord[]>([]);

  const syncWebSearch = useCallback((data: AnyRecord, fallback?: AnyRecord) => {
    setWebSearchDraft(cloneRecord(field<AnyRecord>(data, 'websearch', fallback || {})));
    setWebSearchRuntime(cloneRecord(field<AnyRecord>(data, 'websearch_runtime', {})));
    setWebSearchIssues((Array.isArray(data.issues) ? data.issues as AnyRecord[] : []).filter(issue => String(field(issue, 'path', '')).startsWith('websearch')));
  }, []);

  const reloadWebSearchConfig = useCallback(async () => {
    setStatus('正在加载搜索流水线配置...');
    const data = await loadWebSearchConfig();
    syncWebSearch(data);
    setStatus('就绪');
  }, [setStatus, syncWebSearch]);

  const updateWebSearchPath = useCallback((path: string[], value: unknown) => {
    setWebSearchDraft(current => setNestedValue(current, path, value));
  }, []);

  const rollbackToLegacyTavily = useCallback(() => {
    setWebSearchDraft(current => {
      const next = setNestedValue(current, ['provider'], 'tavily');
      return setNestedValue(next, ['pipeline', 'enabled'], false);
    });
  }, []);

  const saveWebSearchDraft = useCallback(async () => {
    setStatus('正在保存搜索流水线配置...');
    try {
      const effective = await saveWebSearchConfig(webSearchDraft);
      syncWebSearch(effective, webSearchDraft);
      setStatus('搜索流水线配置已保存');
    } catch (error) {
      showError(error);
    }
  }, [setStatus, showError, syncWebSearch, webSearchDraft]);

  return useMemo(() => ({
    webSearchDraft,
    webSearchRuntime,
    webSearchIssues,
    webSearchJSON: pretty(webSearchDraft),
    reloadWebSearchConfig,
    updateWebSearchPath,
    rollbackToLegacyTavily,
    saveWebSearchDraft,
  }), [
    webSearchDraft,
    webSearchRuntime,
    webSearchIssues,
    reloadWebSearchConfig,
    updateWebSearchPath,
    rollbackToLegacyTavily,
    saveWebSearchDraft,
  ]);
}

export type WebSearchAdmin = ReturnType<typeof useWebSearchAdmin>;

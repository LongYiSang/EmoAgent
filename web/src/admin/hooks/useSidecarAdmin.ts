import { useCallback, useMemo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { loadSidecarStatus, sidecarAction } from '../protocol/adminApi';
import type { AdminStatusControls } from './useAdminStatus';

type SidecarAdminOptions = Pick<AdminStatusControls, 'showError'>;

export function useSidecarAdmin({ showError }: SidecarAdminOptions) {
  const [sidecarStatus, setSidecarStatus] = useState<AnyRecord>({});
  const [sidecarGenerated, setSidecarGenerated] = useState('');
  const [sidecarLogs, setSidecarLogs] = useState('');

  const reloadSidecar = useCallback(async () => {
    const next = await loadSidecarStatus();
    setSidecarStatus(next.status);
    setSidecarGenerated(next.generated);
    setSidecarLogs(next.logs);
  }, []);

  const runSidecarAction = useCallback(async (action: 'start' | 'stop' | 'restart') => {
    try {
      await sidecarAction(action);
      await reloadSidecar();
    } catch (error) {
      showError(error);
    }
  }, [reloadSidecar, showError]);

  return useMemo(() => ({
    sidecarStatus,
    sidecarGenerated,
    sidecarLogs,
    reloadSidecar,
    runSidecarAction,
  }), [sidecarStatus, sidecarGenerated, sidecarLogs, reloadSidecar, runSidecarAction]);
}

export type SidecarAdmin = ReturnType<typeof useSidecarAdmin>;

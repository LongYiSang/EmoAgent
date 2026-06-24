import { useCallback, useMemo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { field } from '../../shared/lib/data';
import {
  loadPythonEnvironments,
  loadPythonToolchain,
  probePythonToolchain,
  pythonEnvironmentAction,
  savePythonToolchain,
} from '../protocol/adminApi';
import { cloneRecord, setNestedValue } from '../lib/adminData';
import type { AdminStatusControls } from './useAdminStatus';

type PythonToolchainAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function usePythonToolchainAdmin({ setStatus, showError }: PythonToolchainAdminOptions) {
  const [toolchainDraft, setToolchainDraft] = useState<AnyRecord>({});
  const [probeResult, setProbeResult] = useState<AnyRecord>({});
  const [environments, setEnvironments] = useState<AnyRecord[]>([]);

  const reloadPythonToolchain = useCallback(async () => {
    const data = await loadPythonToolchain();
    setToolchainDraft(cloneRecord(field<AnyRecord>(data, 'python_toolchain', {})));
    setStatus('Python Toolchain 已加载');
  }, [setStatus]);

  const reloadPythonEnvironments = useCallback(async () => {
    setEnvironments(await loadPythonEnvironments());
  }, []);

  const reloadPythonToolchainSurfaces = useCallback(async () => {
    await Promise.all([reloadPythonToolchain(), reloadPythonEnvironments()]);
  }, [reloadPythonToolchain, reloadPythonEnvironments]);

  const updateToolchainPath = useCallback((path: string[], value: unknown) => {
    setToolchainDraft(current => setNestedValue(current, path, value));
  }, []);

  const saveToolchainDraft = useCallback(async () => {
    try {
      const data = await savePythonToolchain(toolchainDraft);
      setToolchainDraft(cloneRecord(field<AnyRecord>(data, 'python_toolchain', toolchainDraft)));
      setStatus('Python Toolchain 已保存');
      await reloadPythonEnvironments();
    } catch (error) {
      showError(error);
    }
  }, [toolchainDraft, reloadPythonEnvironments, setStatus, showError]);

  const probeToolchainDraft = useCallback(async () => {
    try {
      const data = await probePythonToolchain(toolchainDraft);
      setProbeResult(field<AnyRecord>(data, 'result', {}));
      setStatus('Python Toolchain 探测完成');
    } catch (error) {
      showError(error);
    }
  }, [toolchainDraft, setStatus, showError]);

  const runEnvironmentAction = useCallback(async (environment: AnyRecord, action: 'sync' | 'repair') => {
    try {
      const updated = await pythonEnvironmentAction(environment, action);
      setEnvironments(current => replaceEnvironment(current, updated));
      setStatus(action === 'repair' ? 'Python 环境 Repair 完成' : 'Python 环境 Sync 完成');
    } catch (error) {
      showError(error);
    }
  }, [setStatus, showError]);

  return useMemo(() => ({
    toolchainDraft,
    probeResult,
    environments,
    reloadPythonToolchain,
    reloadPythonEnvironments,
    reloadPythonToolchainSurfaces,
    updateToolchainPath,
    saveToolchainDraft,
    probeToolchainDraft,
    runEnvironmentAction,
  }), [
    toolchainDraft,
    probeResult,
    environments,
    reloadPythonToolchain,
    reloadPythonEnvironments,
    reloadPythonToolchainSurfaces,
    updateToolchainPath,
    saveToolchainDraft,
    probeToolchainDraft,
    runEnvironmentAction,
  ]);
}

function replaceEnvironment(current: AnyRecord[], updated: AnyRecord): AnyRecord[] {
  const owner = field<AnyRecord>(updated, 'owner', {});
  const key = environmentKey(owner);
  let replaced = false;
  const next = current.map(item => {
    if (environmentKey(field<AnyRecord>(item, 'owner', {})) !== key) return item;
    replaced = true;
    return updated;
  });
  if (!replaced) next.push(updated);
  return next;
}

function environmentKey(owner: AnyRecord): string {
  return `${String(owner.kind || '')}:${String(owner.id || '')}:${String(owner.version || '')}`;
}

export type PythonToolchainAdmin = ReturnType<typeof usePythonToolchainAdmin>;

import { useCallback, useMemo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { field, pretty } from '../../shared/lib/data';
import {
  loadConfigIssues,
  loadEffectiveConfig,
  loadMemoryExtractions,
  loadMemoryFeatures,
  loadMemorySegments,
  loadNaturalMemoryLatest,
  runNaturalMemory,
  saveMemoryConfig,
  saveMemoryFeatures,
  validateConfig,
} from '../protocol/adminApi';
import { cloneRecord, parseJSONRecord, setNestedValue } from '../lib/adminData';
import type { AdminStatusControls } from './useAdminStatus';

type MemoryAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function useMemoryAdmin({ setStatus, showError }: MemoryAdminOptions) {
  const [effectiveConfig, setEffectiveConfig] = useState<AnyRecord>({});
  const [memoryDraft, setMemoryDraft] = useState<AnyRecord>({});
  const [memoryFeatures, setMemoryFeatures] = useState<AnyRecord>({});
  const [memoryJobs, setMemoryJobs] = useState<AnyRecord[]>([]);
  const [memorySegments, setMemorySegments] = useState<AnyRecord[]>([]);
  const [configIssues, setConfigIssues] = useState<AnyRecord[]>([]);
  const [naturalMemoryLatest, setNaturalMemoryLatest] = useState<AnyRecord>({});
  const [privacyDraft, setPrivacyDraft] = useState('{}');
  const [retentionDraft, setRetentionDraft] = useState('{}');

  const syncEffectiveConfig = useCallback((effective: AnyRecord, fallbackMemory?: AnyRecord) => {
    setEffectiveConfig(effective);
    setConfigIssues(Array.isArray(effective.issues) ? effective.issues as AnyRecord[] : []);
    const memory = cloneRecord(field<AnyRecord>(effective, 'memory', fallbackMemory || {}));
    setMemoryDraft(memory);
    setPrivacyDraft(pretty({
      forgetting_privacy: field(memory, 'forgetting_privacy', field(field(effective, 'memory_core', {}), 'forgetting_privacy', {})),
      agent_affect: field(memory, 'agent_affect', field(field(effective, 'memory_core', {}), 'agent_affect', {})),
    }));
    setRetentionDraft(pretty(field(memory, 'retention', field(field(effective, 'memory_core', {}), 'retention', {}))));
  }, []);

  const reloadEffectiveConfig = useCallback(async () => {
    setStatus('Loading effective config...');
    const effective = await loadEffectiveConfig();
    syncEffectiveConfig(effective);
    setStatus('Ready');
  }, [syncEffectiveConfig, setStatus]);

  const reloadConfigIssues = useCallback(async () => {
    setConfigIssues(await loadConfigIssues());
  }, []);

  const reloadMemorySurfaces = useCallback(async () => {
    const [features, jobs, segments] = await Promise.all([
      loadMemoryFeatures(),
      loadMemoryExtractions(),
      loadMemorySegments(),
    ]);
    setMemoryFeatures(features);
    setMemoryJobs(jobs);
    setMemorySegments(segments);
  }, []);

  const reloadNaturalLatest = useCallback(async () => {
    const latest = await loadNaturalMemoryLatest();
    setNaturalMemoryLatest(latest);
    setStatus('Natural memory latest loaded');
  }, [setStatus]);

  const patchMemoryDraft = useCallback((key: string, value: unknown) => {
    setMemoryDraft(current => ({ ...current, [key]: value }));
  }, []);

  const updateMemoryPath = useCallback((path: string[], value: unknown) => {
    setMemoryDraft(current => setNestedValue(current, path, value));
  }, []);

  const persistMemory = useCallback(async (memory: AnyRecord, label: string) => {
    try {
      const effective = await saveMemoryConfig(memory);
      setEffectiveConfig(effective);
      setMemoryDraft(cloneRecord(field<AnyRecord>(effective, 'memory', memory)));
      setStatus(label);
    } catch (error) {
      showError(error);
    }
  }, [setStatus, showError]);

  const saveMemoryFeaturesDraft = useCallback(async () => {
    try {
      const effective = await saveMemoryFeatures(memoryDraft);
      setEffectiveConfig(effective);
      setStatus('Memory features saved');
    } catch (error) {
      showError(error);
    }
  }, [memoryDraft, setStatus, showError]);

  const saveMemoryCore = useCallback(() => persistMemory(memoryDraft, 'Memory core saved'), [memoryDraft, persistMemory]);
  const savePipelines = useCallback(() => persistMemory(memoryDraft, 'Pipeline bindings saved'), [memoryDraft, persistMemory]);
  const saveRetrieval = useCallback(() => persistMemory(memoryDraft, 'Retrieval saved'), [memoryDraft, persistMemory]);
  const saveSidecarConfig = useCallback(() => persistMemory(memoryDraft, 'Sidecar config saved'), [memoryDraft, persistMemory]);

  const runNaturalMemoryNow = useCallback(async (dryRun: boolean) => {
    try {
      const data = await runNaturalMemory(dryRun);
      setNaturalMemoryLatest(data);
      setStatus(dryRun ? 'Natural memory dry-run completed' : 'Natural memory run completed');
    } catch (error) {
      showError(error);
    }
  }, [setStatus, showError]);

  const savePrivacyForget = useCallback(async () => {
    try {
      const payload = parseJSONRecord(privacyDraft);
      const next = cloneRecord(memoryDraft);
      next.forgetting_privacy = payload.forgetting_privacy || payload;
      if (payload.agent_affect) next.agent_affect = payload.agent_affect;
      setMemoryDraft(next);
      await persistMemory(next, 'Privacy/forget saved');
    } catch (error) {
      showError(error);
    }
  }, [privacyDraft, memoryDraft, persistMemory, showError]);

  const saveRetention = useCallback(async () => {
    try {
      const next = cloneRecord(memoryDraft);
      next.retention = parseJSONRecord(retentionDraft);
      setMemoryDraft(next);
      await persistMemory(next, 'Retention saved');
    } catch (error) {
      showError(error);
    }
  }, [retentionDraft, memoryDraft, persistMemory, showError]);

  const validateEffectiveConfig = useCallback(async () => {
    try {
      const result = await validateConfig();
      setConfigIssues(Array.isArray(result.issues) ? result.issues as AnyRecord[] : []);
      setStatus('Config validated');
    } catch (error) {
      showError(error);
    }
  }, [setStatus, showError]);

  return useMemo(() => ({
    effectiveConfig,
    memoryDraft,
    memoryFeatures,
    memoryJobs,
    memorySegments,
    configIssues,
    naturalMemoryLatest,
    privacyDraft,
    setPrivacyDraft,
    retentionDraft,
    setRetentionDraft,
    reloadEffectiveConfig,
    reloadConfigIssues,
    reloadMemorySurfaces,
    reloadNaturalLatest,
    patchMemoryDraft,
    updateMemoryPath,
    persistMemory,
    saveMemoryFeaturesDraft,
    saveMemoryCore,
    savePipelines,
    saveRetrieval,
    saveSidecarConfig,
    runNaturalMemoryNow,
    savePrivacyForget,
    saveRetention,
    validateEffectiveConfig,
  }), [
    effectiveConfig,
    memoryDraft,
    memoryFeatures,
    memoryJobs,
    memorySegments,
    configIssues,
    naturalMemoryLatest,
    privacyDraft,
    retentionDraft,
    reloadEffectiveConfig,
    reloadConfigIssues,
    reloadMemorySurfaces,
    reloadNaturalLatest,
    patchMemoryDraft,
    updateMemoryPath,
    persistMemory,
    saveMemoryFeaturesDraft,
    saveMemoryCore,
    savePipelines,
    saveRetrieval,
    saveSidecarConfig,
    runNaturalMemoryNow,
    savePrivacyForget,
    saveRetention,
    validateEffectiveConfig,
  ]);
}

export type MemoryAdmin = ReturnType<typeof useMemoryAdmin>;

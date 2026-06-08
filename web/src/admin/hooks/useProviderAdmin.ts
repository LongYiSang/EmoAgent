import { type FormEvent, useCallback, useMemo, useRef, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { stringField } from '../../shared/lib/data';
import {
  deleteProvider,
  loadProviderEnvStatus,
  loadProviderModels,
  loadProviderPresets,
  loadProviders,
  refreshProviderModels,
  saveProvider,
  testProvider,
  type Provider,
  type ProviderPreset,
} from '../protocol/adminApi';
import { cloneRecord, emptyProvider } from '../lib/adminData';
import type { AdminStatusControls } from './useAdminStatus';

type ProviderAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function useProviderAdmin({ setStatus, showError }: ProviderAdminOptions) {
  const [providerPresets, setProviderPresets] = useState<ProviderPreset[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [providerModels, setProviderModels] = useState<Record<string, AnyRecord[]>>({});
  const [providerEnv, setProviderEnv] = useState<AnyRecord>({});
  const [selectedProvider, setSelectedProvider] = useState('');
  const [providerDraft, setProviderDraft] = useState<Provider>(emptyProvider());
  const selectedProviderRef = useRef('');
  const providerDetailRequestRef = useRef(0);

  const modelOptions = useMemo(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const models of Object.values(providerModels)) {
      for (const model of models) {
        const id = stringField(model, 'id') || stringField(model, 'name');
        if (id && !seen.has(id)) {
          seen.add(id);
          out.push(id);
        }
      }
    }
    return out;
  }, [providerModels]);

  const reloadProviderPresets = useCallback(async () => {
    setProviderPresets(await loadProviderPresets());
  }, []);

  const selectProvider = useCallback((id: string, source = providers) => {
    const requestID = ++providerDetailRequestRef.current;
    const provider = source.find(item => item.id === id);
    selectedProviderRef.current = id;
    setSelectedProvider(id);
    setProviderDraft(provider ? cloneRecord(provider) : emptyProvider());
    setProviderEnv({});
    if (id) {
      loadProviderModels(id).then(models => setProviderModels(current => ({ ...current, [id]: models }))).catch(() => undefined);
      loadProviderEnvStatus(id)
        .then(env => {
          if (providerDetailRequestRef.current === requestID && selectedProviderRef.current === id) setProviderEnv(env);
        })
        .catch(() => {
          if (providerDetailRequestRef.current === requestID && selectedProviderRef.current === id) setProviderEnv({});
        });
    }
  }, [providers]);

  const reloadProviders = useCallback(async (preferredID = selectedProvider) => {
    setStatus('Loading providers...');
    const next = await loadProviders();
    setProviders(next);
    if (preferredID && next.some(item => item.id === preferredID)) {
      selectProvider(preferredID, next);
    } else if (next[0]?.id) {
      selectProvider(next[0].id, next);
    } else {
      selectedProviderRef.current = '';
      providerDetailRequestRef.current += 1;
      setSelectedProvider('');
      setProviderDraft(emptyProvider());
      setProviderEnv({});
    }
    setStatus('Ready');
  }, [selectedProvider, selectProvider, setStatus]);

  const patchProviderDraft = useCallback((key: string, value: unknown) => {
    setProviderDraft(current => ({ ...current, [key]: value }));
  }, []);

  const setProviderCapability = useCallback((capability: string, checked: boolean) => {
    setProviderDraft(current => {
      const next = new Set(Array.isArray(current.capabilities) ? current.capabilities : ['chat']);
      if (checked) next.add(capability);
      else next.delete(capability);
      return { ...current, capabilities: Array.from(next) };
    });
  }, []);

  const applyProviderPreset = useCallback((id: string) => {
    const preset = providerPresets.find(item => item.id === id);
    if (!preset) return;
    setProviderDraft(current => ({
      ...current,
      id: selectedProvider || current.id || preset.id,
      name: preset.name || current.name || '',
      preset_id: id,
      protocol: preset.protocol || 'openai_compatible',
      model_discovery: preset.model_discovery || 'manual',
      base_url: preset.base_url || '',
      api_key_env: preset.api_key_env || '',
      capabilities: Array.isArray(preset.capabilities) ? preset.capabilities : ['chat'],
    }));
  }, [providerPresets, selectedProvider]);

  const newProvider = useCallback(() => {
    selectedProviderRef.current = '';
    providerDetailRequestRef.current += 1;
    setSelectedProvider('');
    setProviderDraft(emptyProvider());
    setProviderEnv({});
  }, []);

  const submitProvider = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    try {
      await saveProvider(providerDraft, selectedProvider);
      const nextID = String(providerDraft.id || selectedProvider);
      selectedProviderRef.current = nextID;
      setSelectedProvider(nextID);
      await reloadProviders(nextID);
      setStatus('Provider saved');
    } catch (error) {
      showError(error);
    }
  }, [providerDraft, selectedProvider, reloadProviders, setStatus, showError]);

  const refreshSelectedProviderModels = useCallback(async () => {
    if (!selectedProvider) return;
    const id = selectedProvider;
    try {
      const models = await refreshProviderModels(id);
      setProviderModels(current => ({ ...current, [id]: models }));
      if (selectedProviderRef.current === id) setStatus('Models refreshed');
    } catch (error) {
      if (selectedProviderRef.current === id) showError(error);
    }
  }, [selectedProvider, setStatus, showError]);

  const testSelectedProvider = useCallback(async () => {
    if (!selectedProvider) return;
    const id = selectedProvider;
    const requestID = ++providerDetailRequestRef.current;
    try {
      const result = await testProvider(id);
      if (providerDetailRequestRef.current !== requestID || selectedProviderRef.current !== id) return;
      setProviderEnv(result);
      setStatus(result.ok ? 'Provider test passed' : 'Provider test failed');
    } catch (error) {
      if (providerDetailRequestRef.current === requestID && selectedProviderRef.current === id) showError(error);
    }
  }, [selectedProvider, setStatus, showError]);

  const deleteSelectedProvider = useCallback(async () => {
    if (!selectedProvider || !window.confirm(`Delete provider "${selectedProvider}"?`)) return;
    try {
      await deleteProvider(selectedProvider);
      await reloadProviders('');
    } catch (error) {
      showError(error);
    }
  }, [selectedProvider, reloadProviders, showError]);

  return useMemo(() => ({
    providerPresets,
    providers,
    providerModels,
    providerEnv,
    selectedProvider,
    providerDraft,
    modelOptions,
    reloadProviderPresets,
    reloadProviders,
    selectProvider,
    patchProviderDraft,
    setProviderCapability,
    applyProviderPreset,
    newProvider,
    submitProvider,
    refreshSelectedProviderModels,
    testSelectedProvider,
    deleteSelectedProvider,
  }), [
    providerPresets,
    providers,
    providerModels,
    providerEnv,
    selectedProvider,
    providerDraft,
    modelOptions,
    reloadProviderPresets,
    reloadProviders,
    selectProvider,
    patchProviderDraft,
    setProviderCapability,
    applyProviderPreset,
    newProvider,
    submitProvider,
    refreshSelectedProviderModels,
    testSelectedProvider,
    deleteSelectedProvider,
  ]);
}

export type ProviderAdmin = ReturnType<typeof useProviderAdmin>;

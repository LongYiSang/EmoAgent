import { type FormEvent, useCallback, useMemo, useRef, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { pretty } from '../../shared/lib/data';
import {
  deletePersona,
  loadDefaultProgressPhrases,
  loadPersona,
  loadPersonas,
  loadProgressPhrases,
  savePersona,
  saveProgressPhrases,
  type Persona,
} from '../protocol/adminApi';
import { cloneRecord, emptyPersona, parseJSONRecord } from '../lib/adminData';
import type { AdminStatusControls } from './useAdminStatus';

type PersonaAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function usePersonaAdmin({ setStatus, showError }: PersonaAdminOptions) {
  const [personas, setPersonas] = useState<Persona[]>([]);
  const [selectedPersona, setSelectedPersona] = useState('');
  const [personaDraft, setPersonaDraft] = useState<Persona>(emptyPersona());
  const [progressDraft, setProgressDraft] = useState<AnyRecord>({});
  const [progressDraftJSON, setProgressDraftJSON] = useState(pretty({}));
  const [progressDraftError, setProgressDraftError] = useState('');
  const [progressDefaults, setProgressDefaults] = useState<AnyRecord>({});
  const personaDetailRequestRef = useRef(0);

  const syncProgressDraft = useCallback((phrases: AnyRecord) => {
    setProgressDraft(phrases);
    setProgressDraftJSON(pretty(phrases));
    setProgressDraftError('');
  }, []);

  const selectPersonaDetail = useCallback(async (key: string, source = personas) => {
    const requestID = ++personaDetailRequestRef.current;
    const summary = source.find(item => item.key === key);
    setSelectedPersona(key);
    if (!key) {
      setPersonaDraft(emptyPersona());
      syncProgressDraft({});
      return;
    }
    try {
      const [detail, phrases] = await Promise.all([loadPersona(key), loadProgressPhrases(key)]);
      if (personaDetailRequestRef.current !== requestID) return;
      setPersonaDraft(detail);
      syncProgressDraft(phrases);
    } catch {
      if (personaDetailRequestRef.current !== requestID) return;
      setPersonaDraft(summary ? cloneRecord(summary) : emptyPersona());
      syncProgressDraft({});
    }
  }, [personas, syncProgressDraft]);

  const reloadPersonas = useCallback(async (preferredKey = selectedPersona) => {
    setStatus('Loading personas...');
    const next = await loadPersonas();
    setPersonas(next);
    const preferred = preferredKey && next.some(item => item.key === preferredKey) ? preferredKey : next[0]?.key || '';
    if (preferred) await selectPersonaDetail(preferred, next);
    else {
      personaDetailRequestRef.current += 1;
      setSelectedPersona('');
      setPersonaDraft(emptyPersona());
      syncProgressDraft({});
    }
    setStatus('Ready');
  }, [selectedPersona, selectPersonaDetail, setStatus, syncProgressDraft]);

  const reloadProgressDefaults = useCallback(async () => {
    setProgressDefaults(await loadDefaultProgressPhrases());
  }, []);

  const selectPersona = useCallback((key: string) => {
    selectPersonaDetail(key).catch(showError);
  }, [selectPersonaDetail, showError]);

  const patchPersonaDraft = useCallback((key: string, value: unknown) => {
    setPersonaDraft(current => ({ ...current, [key]: value }));
  }, []);

  const patchProgressDraftJSON = useCallback((value: string) => {
    setProgressDraftJSON(value);
    setProgressDraftError('');
  }, []);

  const newPersona = useCallback(() => {
    personaDetailRequestRef.current += 1;
    setSelectedPersona('');
    setPersonaDraft(emptyPersona());
    syncProgressDraft({});
  }, [syncProgressDraft]);

  const submitPersona = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    let parsedProgress: AnyRecord;
    try {
      parsedProgress = parseJSONRecord(progressDraftJSON);
      setProgressDraft(parsedProgress);
      setProgressDraftError('');
    } catch (error) {
      setProgressDraftError(error instanceof Error ? error.message : String(error));
      showError(error);
      return;
    }
    try {
      await savePersona(personaDraft, selectedPersona);
      const key = String(personaDraft.key || selectedPersona || '');
      if (key) await saveProgressPhrases(key, parsedProgress);
      setSelectedPersona(key);
      await reloadPersonas(key);
      setStatus('Persona saved');
    } catch (error) {
      showError(error);
    }
  }, [personaDraft, selectedPersona, progressDraftJSON, reloadPersonas, setStatus, showError]);

  const deleteSelectedPersona = useCallback(async () => {
    if (!selectedPersona || !window.confirm(`Delete persona "${selectedPersona}"?`)) return;
    try {
      await deletePersona(selectedPersona);
      await reloadPersonas('');
    } catch (error) {
      showError(error);
    }
  }, [selectedPersona, reloadPersonas, showError]);

  return useMemo(() => ({
    personas,
    selectedPersona,
    personaDraft,
    progressDraft,
    progressDraftJSON,
    progressDraftError,
    progressDefaults,
    reloadPersonas,
    reloadProgressDefaults,
    selectPersona,
    patchPersonaDraft,
    patchProgressDraftJSON,
    newPersona,
    submitPersona,
    deleteSelectedPersona,
  }), [
    personas,
    selectedPersona,
    personaDraft,
    progressDraft,
    progressDraftJSON,
    progressDraftError,
    progressDefaults,
    reloadPersonas,
    reloadProgressDefaults,
    selectPersona,
    patchPersonaDraft,
    patchProgressDraftJSON,
    newPersona,
    submitPersona,
    deleteSelectedPersona,
  ]);
}

export type PersonaAdmin = ReturnType<typeof usePersonaAdmin>;

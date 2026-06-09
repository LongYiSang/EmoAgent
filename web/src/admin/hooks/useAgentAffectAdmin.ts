import { useCallback, useMemo, useState } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { field, pretty } from '../../shared/lib/data';
import { cloneRecord, setNestedValue } from '../lib/adminData';
import type { AdminStatusControls } from './useAdminStatus';
import {
  applyAgentAffectDelta,
  evaluateAgentAffect,
  loadAgentAffectConfig,
  loadAgentAffectCurrent,
  loadAgentAffectHistory,
  loadAgentAffectPluginWrites,
  loadAgentAffectProfile,
  previewAgentAffectPrompt,
  resetAgentAffect,
  saveAgentAffectConfig,
  saveAgentAffectProfile,
  submitAgentAffect,
} from '../protocol/agentAffectApi';

type AgentAffectOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function useAgentAffectAdmin({ setStatus, showError }: AgentAffectOptions) {
  const [personaID, setPersonaID] = useState('default');
  const [sessionID, setSessionID] = useState('admin-debug');
  const [configDraft, setConfigDraft] = useState<AnyRecord>({});
  const [profileDraft, setProfileDraft] = useState<AnyRecord>({});
  const [currentMood, setCurrentMood] = useState<AnyRecord>({});
  const [history, setHistory] = useState<AnyRecord>({ evaluations: [], events: [] });
  const [pluginWrites, setPluginWrites] = useState<AnyRecord[]>([]);
  const [promptPreview, setPromptPreview] = useState('');
  const [debugInput, setDebugInput] = useState('用户表达了感谢，希望继续深入讨论。');
  const [deltaJSON, setDeltaJSON] = useState('{\n  "warmth": 0.1,\n  "curiosity": 0.05\n}');
  const [lastResult, setLastResult] = useState<AnyRecord>({});

  const syncConfig = useCallback((data: AnyRecord) => {
    setConfigDraft(cloneRecord(field<AnyRecord>(data, 'agent_affect', {})));
  }, []);

  const reloadAgentAffect = useCallback(async () => {
    setStatus('正在加载 Agent Affect...');
    const [cfg, current, hist, profile, writes, prompt] = await Promise.all([
      loadAgentAffectConfig(),
      loadAgentAffectCurrent({ personaID, sessionID }),
      loadAgentAffectHistory({ personaID, sessionID, kind: 'both', limit: 30 }),
      loadAgentAffectProfile(personaID),
      loadAgentAffectPluginWrites({ personaID, sessionID, limit: 30 }),
      previewAgentAffectPrompt({ persona_id: personaID, session_id: sessionID }),
    ]);
    syncConfig(cfg);
    setCurrentMood(current);
    setHistory(hist);
    setProfileDraft(profile);
    setPluginWrites(writes);
    setPromptPreview(String(field(prompt, 'prompt_block', '')));
    setStatus('就绪');
  }, [personaID, sessionID, setStatus, syncConfig]);

  const reloadAgentAffectState = useCallback(async () => {
    const [current, hist, writes, prompt] = await Promise.all([
      loadAgentAffectCurrent({ personaID, sessionID }),
      loadAgentAffectHistory({ personaID, sessionID, kind: 'both', limit: 30 }),
      loadAgentAffectPluginWrites({ personaID, sessionID, limit: 30 }),
      previewAgentAffectPrompt({ persona_id: personaID, session_id: sessionID }),
    ]);
    setCurrentMood(current);
    setHistory(hist);
    setPluginWrites(writes);
    setPromptPreview(String(field(prompt, 'prompt_block', '')));
  }, [personaID, sessionID]);

  const updateConfigPath = useCallback((path: string[], value: unknown) => {
    setConfigDraft(current => setNestedValue(current, path, value));
  }, []);

  const updateProfileBaseline = useCallback((key: string, value: number) => {
    setProfileDraft(current => setNestedValue(current, ['baseline', key], value));
  }, []);

  const saveConfigDraft = useCallback(async () => {
    setStatus('正在保存 Agent Affect 配置...');
    try {
      const effective = await saveAgentAffectConfig(configDraft);
      setConfigDraft(cloneRecord(field<AnyRecord>(effective, 'agent_affect', configDraft)));
      await reloadAgentAffectState();
      setStatus('Agent Affect 配置已保存');
    } catch (error) {
      showError(error);
    }
  }, [configDraft, reloadAgentAffectState, setStatus, showError]);

  const saveProfileDraft = useCallback(async () => {
    setStatus('正在保存 Agent Affect Profile...');
    try {
      const saved = await saveAgentAffectProfile({ ...profileDraft, persona_id: personaID, profile_name: field(profileDraft, 'profile_name', 'default') });
      setProfileDraft(saved);
      await reloadAgentAffectState();
      setStatus('Agent Affect Profile 已保存');
    } catch (error) {
      showError(error);
    }
  }, [personaID, profileDraft, reloadAgentAffectState, setStatus, showError]);

  const buildImpactRequest = useCallback((commitMode: string) => ({
    persona_id: personaID,
    session_id: sessionID,
    trigger: { trigger_type: 'debug', source_kind: 'admin' },
    input: { mode: 'summary', summary: debugInput },
    commit_mode: commitMode,
  }), [debugInput, personaID, sessionID]);

  const evaluatePreview = useCallback(async () => {
    setStatus('正在评估 mood impact...');
    try {
      const result = await evaluateAgentAffect(buildImpactRequest('preview'));
      setLastResult(result);
      await reloadAgentAffectState();
      setStatus('评估完成');
    } catch (error) {
      showError(error);
    }
  }, [buildImpactRequest, reloadAgentAffectState, setStatus, showError]);

  const submitCommit = useCallback(async () => {
    setStatus('正在提交 mood impact...');
    try {
      const result = await submitAgentAffect(buildImpactRequest('commit_if_allowed'));
      setLastResult(result);
      await reloadAgentAffectState();
      setStatus('mood impact 已提交');
    } catch (error) {
      showError(error);
    }
  }, [buildImpactRequest, reloadAgentAffectState, setStatus, showError]);

  const applyDelta = useCallback(async () => {
    setStatus('正在写入 mood delta...');
    try {
      const delta = JSON.parse(deltaJSON || '{}');
      const result = await applyAgentAffectDelta({
        persona_id: personaID,
        session_id: sessionID,
        trigger: { trigger_type: 'debug', source_kind: 'admin' },
        delta,
        committed_by: 'user_debug',
      });
      setLastResult(result);
      await reloadAgentAffectState();
      setStatus('mood delta 已写入');
    } catch (error) {
      showError(error);
    }
  }, [deltaJSON, personaID, reloadAgentAffectState, sessionID, setStatus, showError]);

  const resetMood = useCallback(async () => {
    setStatus('正在重置 Agent Affect mood...');
    try {
      const result = await resetAgentAffect({ persona_id: personaID, session_id: sessionID, reason: 'Admin reset' });
      setLastResult(result);
      await reloadAgentAffectState();
      setStatus('Agent Affect mood 已重置');
    } catch (error) {
      showError(error);
    }
  }, [personaID, reloadAgentAffectState, sessionID, setStatus, showError]);

  const refreshPromptPreview = useCallback(async () => {
    try {
      const result = await previewAgentAffectPrompt({ persona_id: personaID, session_id: sessionID });
      setPromptPreview(String(field(result, 'prompt_block', '')));
    } catch (error) {
      showError(error);
    }
  }, [personaID, sessionID, showError]);

  return useMemo(() => ({
    personaID,
    sessionID,
    configDraft,
    profileDraft,
    currentMood,
    history,
    pluginWrites,
    promptPreview,
    debugInput,
    deltaJSON,
    lastResult,
    setPersonaID,
    setSessionID,
    setDebugInput,
    setDeltaJSON,
    reloadAgentAffect,
    reloadAgentAffectState,
    updateConfigPath,
    updateProfileBaseline,
    setProfileDraft,
    saveConfigDraft,
    saveProfileDraft,
    evaluatePreview,
    submitCommit,
    applyDelta,
    resetMood,
    refreshPromptPreview,
    configJSON: pretty(configDraft),
    resultJSON: pretty(lastResult),
  }), [personaID, sessionID, configDraft, profileDraft, currentMood, history, pluginWrites, promptPreview, debugInput, deltaJSON, lastResult, reloadAgentAffect, reloadAgentAffectState, updateConfigPath, updateProfileBaseline, saveConfigDraft, saveProfileDraft, evaluatePreview, submitCommit, applyDelta, resetMood, refreshPromptPreview]);
}

export type AgentAffectAdmin = ReturnType<typeof useAgentAffectAdmin>;

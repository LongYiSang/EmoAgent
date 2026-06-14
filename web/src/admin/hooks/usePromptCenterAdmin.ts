import { useCallback, useMemo, useState } from 'react';
import type { AdminStatusControls } from './useAdminStatus';
import {
  deletePromptOverride,
  loadPromptComponents,
  loadPromptSnapshot,
  loadPromptSnapshots,
  previewPrompt,
  savePromptOverride,
  type PromptComponentDetail,
  type PromptLintWarning,
  type PromptPreviewResponse,
  type PromptSnapshotDetail,
  type PromptSnapshotSummary,
} from '../protocol/promptCenterApi';

type PromptCenterAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function usePromptCenterAdmin({ setStatus, showError }: PromptCenterAdminOptions) {
  const [components, setComponents] = useState<PromptComponentDetail[]>([]);
  const [selectedComponentID, setSelectedComponentID] = useState('');
  const [selectedAgentID, setSelectedAgentIDState] = useState('');
  const [globalDraft, setGlobalDraft] = useState('');
  const [agentDraft, setAgentDraft] = useState('');
  const [showOverriddenOnly, setShowOverriddenOnly] = useState(false);
  const [preview, setPreview] = useState<PromptPreviewResponse | null>(null);
  const [lintWarnings, setLintWarnings] = useState<PromptLintWarning[]>([]);
  const [snapshots, setSnapshots] = useState<PromptSnapshotSummary[]>([]);
  const [selectedSnapshotID, setSelectedSnapshotID] = useState('');
  const [snapshotDetail, setSnapshotDetail] = useState<PromptSnapshotDetail | null>(null);
  const [fullPreviewSessionID, setFullPreviewSessionID] = useState('');
  const [fullPreviewUserMessage, setFullPreviewUserMessage] = useState('');
  const [includeMemoryPreview, setIncludeMemoryPreview] = useState(false);
  const [includeAgentAffectPreview, setIncludeAgentAffectPreview] = useState(false);

  const selectedComponent = useMemo(
    () => components.find(item => item.id === selectedComponentID) || null,
    [components, selectedComponentID],
  );

  const applyDrafts = useCallback((component: PromptComponentDetail | null) => {
    setGlobalDraft(component?.global_override?.override_text || '');
    setAgentDraft(component?.agent_override?.mode === 'custom' ? component.agent_override.override_text : '');
  }, []);

  const selectPromptComponent = useCallback((id: string, source = components) => {
    setSelectedComponentID(id);
    setLintWarnings([]);
    applyDrafts(source.find(item => item.id === id) || null);
  }, [applyDrafts, components]);

  const confirmProtocolSensitiveSave = useCallback(() => {
    if (selectedComponent?.risk_level !== 'protocol_sensitive') return true;
    return window.confirm('此提示词会影响协议、工具调用或 JSON 输出。确认保存当前修改？');
  }, [selectedComponent]);

  const reloadPromptCenter = useCallback(async (agentID = selectedAgentID) => {
    setStatus('正在加载提示词中心...');
    const data = await loadPromptComponents(agentID);
    setComponents(data.components);
    const preferred = selectedComponentID && data.components.some(item => item.id === selectedComponentID)
      ? selectedComponentID
      : data.components[0]?.id || '';
    if (preferred) selectPromptComponent(preferred, data.components);
    setStatus('就绪');
  }, [selectedAgentID, selectedComponentID, selectPromptComponent, setStatus]);

  const setPromptAgentID = useCallback(async (agentID: string) => {
    setSelectedAgentIDState(agentID);
    try {
      await reloadPromptCenter(agentID);
      const nextSnapshots = await loadPromptSnapshots({ agent_id: agentID, limit: 20 });
      setSnapshots(nextSnapshots);
      setSelectedSnapshotID('');
      setSnapshotDetail(null);
    } catch (error) {
      showError(error);
    }
  }, [reloadPromptCenter, showError]);

  const reloadPromptSnapshots = useCallback(async (agentID = selectedAgentID) => {
    try {
      const items = await loadPromptSnapshots({ agent_id: agentID, limit: 20 });
      setSnapshots(items);
      if (selectedSnapshotID && !items.some(item => item.id === selectedSnapshotID)) {
        setSelectedSnapshotID('');
        setSnapshotDetail(null);
      }
    } catch (error) {
      showError(error);
    }
  }, [selectedAgentID, selectedSnapshotID, showError]);

  const selectPromptSnapshot = useCallback(async (id: string) => {
    try {
      setSelectedSnapshotID(id);
      setSnapshotDetail(await loadPromptSnapshot(id));
    } catch (error) {
      showError(error);
    }
  }, [showError]);

  const saveGlobalOverride = useCallback(async () => {
    if (!selectedComponent) return;
    if (!confirmProtocolSensitiveSave()) return;
    try {
      const resp = await savePromptOverride({
        component_id: selectedComponent.id,
        scope_type: 'global',
        mode: 'custom',
        override_text: globalDraft,
      });
      await reloadPromptCenter(selectedAgentID);
      setLintWarnings(resp.warnings || []);
      setStatus('全局覆盖已保存');
    } catch (error) {
      showError(error);
    }
  }, [confirmProtocolSensitiveSave, globalDraft, reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

  const resetGlobalOverride = useCallback(async () => {
    if (!selectedComponent) return;
    try {
      await deletePromptOverride(selectedComponent.id, 'global');
      setLintWarnings([]);
      await reloadPromptCenter(selectedAgentID);
      setStatus('全局已恢复内置默认');
    } catch (error) {
      showError(error);
    }
  }, [reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

  const saveAgentOverride = useCallback(async () => {
    if (!selectedComponent || !selectedAgentID) return;
    if (!confirmProtocolSensitiveSave()) return;
    try {
      const resp = await savePromptOverride({
        component_id: selectedComponent.id,
        scope_type: 'agent',
        scope_id: selectedAgentID,
        mode: 'custom',
        override_text: agentDraft,
      });
      await reloadPromptCenter(selectedAgentID);
      setLintWarnings(resp.warnings || []);
      setStatus('Agent 自定义已保存');
    } catch (error) {
      showError(error);
    }
  }, [agentDraft, confirmProtocolSensitiveSave, reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

  const inheritGlobal = useCallback(async () => {
    if (!selectedComponent || !selectedAgentID) return;
    try {
      await deletePromptOverride(selectedComponent.id, 'agent', selectedAgentID);
      setLintWarnings([]);
      await reloadPromptCenter(selectedAgentID);
      setStatus('Agent 已继承全局设置');
    } catch (error) {
      showError(error);
    }
  }, [reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

  const useEmbeddedDefault = useCallback(async () => {
    if (!selectedComponent || !selectedAgentID) return;
    if (!confirmProtocolSensitiveSave()) return;
    try {
      const resp = await savePromptOverride({
        component_id: selectedComponent.id,
        scope_type: 'agent',
        scope_id: selectedAgentID,
        mode: 'use_default',
      });
      await reloadPromptCenter(selectedAgentID);
      setLintWarnings(resp.warnings || []);
      setStatus('Agent 已使用内置默认');
    } catch (error) {
      showError(error);
    }
  }, [confirmProtocolSensitiveSave, reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

  const previewEffectivePrompt = useCallback(async () => {
    if (!selectedComponent) return;
    try {
      setPreview(await previewPrompt({
        agent_id: selectedAgentID,
        purpose: 'emotion_chat',
        component_ids: [selectedComponent.id],
      }));
      setStatus('预览已更新');
    } catch (error) {
      showError(error);
    }
  }, [selectedAgentID, selectedComponent, setStatus, showError]);

  const previewFullEmotionPrompt = useCallback(async () => {
    if (!selectedAgentID) return;
    try {
      setPreview(await previewPrompt({
        mode: 'full',
        agent_id: selectedAgentID,
        persona_key: '',
        purpose: 'emotion_chat_full',
        session_id: fullPreviewSessionID,
        user_message: fullPreviewUserMessage,
        include_memory: includeMemoryPreview,
        include_agent_affect: includeAgentAffectPreview,
      }));
      setStatus('完整 Emotion prompt 预览已更新');
    } catch (error) {
      showError(error);
    }
  }, [fullPreviewSessionID, fullPreviewUserMessage, includeAgentAffectPreview, includeMemoryPreview, selectedAgentID, setStatus, showError]);

  const visibleComponents = useMemo(() => {
    if (!showOverriddenOnly) return components;
    return components.filter(item => item.global_override || item.agent_override);
  }, [components, showOverriddenOnly]);

  return useMemo(() => ({
    components,
    visibleComponents,
    selectedComponentID,
    selectedComponent,
    selectedAgentID,
    globalDraft,
    agentDraft,
    showOverriddenOnly,
    preview,
    lintWarnings,
    snapshots,
    selectedSnapshotID,
    snapshotDetail,
    fullPreviewSessionID,
    fullPreviewUserMessage,
    includeMemoryPreview,
    includeAgentAffectPreview,
    reloadPromptCenter,
    setPromptAgentID,
    selectPromptComponent,
    selectPromptSnapshot,
    setGlobalDraft,
    setAgentDraft,
    setShowOverriddenOnly,
    setFullPreviewSessionID,
    setFullPreviewUserMessage,
    setIncludeMemoryPreview,
    setIncludeAgentAffectPreview,
    saveGlobalOverride,
    resetGlobalOverride,
    saveAgentOverride,
    inheritGlobal,
    useEmbeddedDefault,
    previewEffectivePrompt,
    previewFullEmotionPrompt,
    reloadPromptSnapshots,
  }), [
    components,
    visibleComponents,
    selectedComponentID,
    selectedComponent,
    selectedAgentID,
    globalDraft,
    agentDraft,
    showOverriddenOnly,
    preview,
    lintWarnings,
    snapshots,
    selectedSnapshotID,
    snapshotDetail,
    fullPreviewSessionID,
    fullPreviewUserMessage,
    includeMemoryPreview,
    includeAgentAffectPreview,
    reloadPromptCenter,
    setPromptAgentID,
    selectPromptComponent,
    selectPromptSnapshot,
    saveGlobalOverride,
    resetGlobalOverride,
    saveAgentOverride,
    inheritGlobal,
    useEmbeddedDefault,
    previewEffectivePrompt,
    previewFullEmotionPrompt,
    reloadPromptSnapshots,
  ]);
}

export type PromptCenterAdmin = ReturnType<typeof usePromptCenterAdmin>;

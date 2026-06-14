import { useCallback, useMemo, useState } from 'react';
import type { AdminStatusControls } from './useAdminStatus';
import {
  deletePromptOverride,
  loadPromptComponents,
  loadPromptSnapshots,
  previewPrompt,
  savePromptOverride,
  type PromptComponentDetail,
  type PromptPreviewResponse,
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
  const [snapshots, setSnapshots] = useState<PromptSnapshotSummary[]>([]);

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
    applyDrafts(source.find(item => item.id === id) || null);
  }, [applyDrafts, components]);

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
    } catch (error) {
      showError(error);
    }
  }, [reloadPromptCenter, showError]);

  const reloadPromptSnapshots = useCallback(async (agentID = selectedAgentID) => {
    try {
      setSnapshots(await loadPromptSnapshots({ agent_id: agentID, limit: 20 }));
    } catch (error) {
      showError(error);
    }
  }, [selectedAgentID, showError]);

  const saveGlobalOverride = useCallback(async () => {
    if (!selectedComponent) return;
    try {
      await savePromptOverride({
        component_id: selectedComponent.id,
        scope_type: 'global',
        mode: 'custom',
        override_text: globalDraft,
      });
      await reloadPromptCenter(selectedAgentID);
      setStatus('全局覆盖已保存');
    } catch (error) {
      showError(error);
    }
  }, [globalDraft, reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

  const resetGlobalOverride = useCallback(async () => {
    if (!selectedComponent) return;
    try {
      await deletePromptOverride(selectedComponent.id, 'global');
      await reloadPromptCenter(selectedAgentID);
      setStatus('全局已恢复内置默认');
    } catch (error) {
      showError(error);
    }
  }, [reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

  const saveAgentOverride = useCallback(async () => {
    if (!selectedComponent || !selectedAgentID) return;
    try {
      await savePromptOverride({
        component_id: selectedComponent.id,
        scope_type: 'agent',
        scope_id: selectedAgentID,
        mode: 'custom',
        override_text: agentDraft,
      });
      await reloadPromptCenter(selectedAgentID);
      setStatus('Agent 自定义已保存');
    } catch (error) {
      showError(error);
    }
  }, [agentDraft, reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

  const inheritGlobal = useCallback(async () => {
    if (!selectedComponent || !selectedAgentID) return;
    try {
      await deletePromptOverride(selectedComponent.id, 'agent', selectedAgentID);
      await reloadPromptCenter(selectedAgentID);
      setStatus('Agent 已继承全局设置');
    } catch (error) {
      showError(error);
    }
  }, [reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

  const useEmbeddedDefault = useCallback(async () => {
    if (!selectedComponent || !selectedAgentID) return;
    try {
      await savePromptOverride({
        component_id: selectedComponent.id,
        scope_type: 'agent',
        scope_id: selectedAgentID,
        mode: 'use_default',
      });
      await reloadPromptCenter(selectedAgentID);
      setStatus('Agent 已使用内置默认');
    } catch (error) {
      showError(error);
    }
  }, [reloadPromptCenter, selectedAgentID, selectedComponent, setStatus, showError]);

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
    snapshots,
    reloadPromptCenter,
    setPromptAgentID,
    selectPromptComponent,
    setGlobalDraft,
    setAgentDraft,
    setShowOverriddenOnly,
    saveGlobalOverride,
    resetGlobalOverride,
    saveAgentOverride,
    inheritGlobal,
    useEmbeddedDefault,
    previewEffectivePrompt,
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
    snapshots,
    reloadPromptCenter,
    setPromptAgentID,
    selectPromptComponent,
    saveGlobalOverride,
    resetGlobalOverride,
    saveAgentOverride,
    inheritGlobal,
    useEmbeddedDefault,
    previewEffectivePrompt,
    reloadPromptSnapshots,
  ]);
}

export type PromptCenterAdmin = ReturnType<typeof usePromptCenterAdmin>;

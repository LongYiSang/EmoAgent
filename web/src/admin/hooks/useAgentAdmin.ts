import { type FormEvent, useCallback, useMemo, useState } from 'react';
import { field } from '../../shared/lib/data';
import {
  activateAgent,
  deleteAgent,
  loadAgents,
  saveAgent,
  type AgentConfig,
} from '../protocol/adminApi';
import { cloneRecord, emptyAgent, setNestedValue } from '../lib/adminData';
import type { AdminStatusControls } from './useAdminStatus';

type AgentAdminOptions = Pick<AdminStatusControls, 'setStatus' | 'showError'>;

export function useAgentAdmin({ setStatus, showError }: AgentAdminOptions) {
  const [agents, setAgents] = useState<AgentConfig[]>([]);
  const [activeAgentID, setActiveAgentID] = useState('');
  const [selectedAgent, setSelectedAgent] = useState('');
  const [agentDraft, setAgentDraft] = useState<AgentConfig>(emptyAgent());

  const activePersona = useMemo(() => String(field(agents.find(item => item.id === activeAgentID), 'persona_key', '')), [agents, activeAgentID]);

  const selectAgent = useCallback((id: string, source = agents) => {
    const agent = source.find(item => item.id === id);
    setSelectedAgent(id);
    setAgentDraft(agent ? cloneRecord(agent) : emptyAgent());
  }, [agents]);

  const reloadAgents = useCallback(async (preferredID = selectedAgent) => {
    setStatus('正在加载 Agent 配置...');
    const next = await loadAgents();
    setAgents(next.configs);
    setActiveAgentID(next.activeID);
    const preferred = preferredID && next.configs.some(item => item.id === preferredID) ? preferredID : next.activeID || next.configs[0]?.id || '';
    selectAgent(preferred, next.configs);
    setStatus('就绪');
  }, [selectedAgent, selectAgent, setStatus]);

  const patchAgentDraft = useCallback((key: string, value: unknown) => {
    setAgentDraft(current => ({ ...current, [key]: value }));
  }, []);

  const updateAgentPath = useCallback((path: string[], value: unknown) => {
    setAgentDraft(current => setNestedValue(current, path, value));
  }, []);

  const replaceAgentDraft = useCallback((draft: AgentConfig) => {
    setAgentDraft(draft);
  }, []);

  const newAgent = useCallback(() => {
    setSelectedAgent('');
    setAgentDraft(emptyAgent());
  }, []);

  const submitAgent = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    try {
      await saveAgent(agentDraft, selectedAgent);
      const nextID = String(agentDraft.id || selectedAgent);
      setSelectedAgent(nextID);
      await reloadAgents(nextID);
      setStatus('Agent 配置已保存');
    } catch (error) {
      showError(error);
    }
  }, [agentDraft, selectedAgent, reloadAgents, setStatus, showError]);

  const activateSelectedAgent = useCallback(async () => {
    if (!selectedAgent) return;
    try {
      await activateAgent(selectedAgent);
      await reloadAgents(selectedAgent);
    } catch (error) {
      showError(error);
    }
  }, [selectedAgent, reloadAgents, showError]);

  const deleteSelectedAgent = useCallback(async () => {
    if (!selectedAgent || !window.confirm(`删除 Agent 配置 "${selectedAgent}"？`)) return;
    try {
      await deleteAgent(selectedAgent);
      await reloadAgents('');
    } catch (error) {
      showError(error);
    }
  }, [selectedAgent, reloadAgents, showError]);

  return useMemo(() => ({
    agents,
    activeAgentID,
    activePersona,
    selectedAgent,
    agentDraft,
    reloadAgents,
    selectAgent,
    patchAgentDraft,
    updateAgentPath,
    replaceAgentDraft,
    newAgent,
    submitAgent,
    activateSelectedAgent,
    deleteSelectedAgent,
  }), [
    agents,
    activeAgentID,
    activePersona,
    selectedAgent,
    agentDraft,
    reloadAgents,
    selectAgent,
    patchAgentDraft,
    updateAgentPath,
    replaceAgentDraft,
    newAgent,
    submitAgent,
    activateSelectedAgent,
    deleteSelectedAgent,
  ]);
}

export type AgentAdmin = ReturnType<typeof useAgentAdmin>;

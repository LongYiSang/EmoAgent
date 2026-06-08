import { memo, useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { field, toInt } from '../../shared/lib/data';
import type { AgentAdmin } from '../hooks/useAgentAdmin';
import type { Persona, Provider, ProviderPreset } from '../protocol/adminApi';
import { matchesQuery, slotDefs } from '../lib/adminData';
import { Field } from '../components/Field';
import { ListPane } from '../components/ListPane';
import { SlotEditor } from '../components/SlotEditor';

export type AgentsTabProps = Pick<AgentAdmin,
  'agents' |
  'activeAgentID' |
  'selectedAgent' |
  'agentDraft' |
  'reloadAgents' |
  'selectAgent' |
  'patchAgentDraft' |
  'updateAgentPath' |
  'replaceAgentDraft' |
  'newAgent' |
  'submitAgent' |
  'activateSelectedAgent' |
  'deleteSelectedAgent'
> & {
  providers: Provider[];
  providerPresets: ProviderPreset[];
  modelOptions: string[];
  personas: Persona[];
};

export default memo(function AgentsTab({
  agents,
  activeAgentID,
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
  providers,
  providerPresets,
  modelOptions,
  personas,
}: AgentsTabProps) {
  const [query, setQuery] = useState('');
  const visibleAgents = agents.filter(agent => matchesQuery(query, agent.id, agent.name, agent.persona_key));

  return (
    <div className="admin-split">
      <ListPane title="Agent 配置" count={`${agents.length} 个配置 · 当前：${activeAgentID || '无'}`} searchID="agent-search" searchValue={query} searchLabel="Agent 配置" onSearch={setQuery} onNew={newAgent} onReload={reloadAgents}>
        {visibleAgents.map(agent => <button className={classNames('item', selectedAgent === agent.id && 'active')} type="button" key={agent.id} onClick={() => selectAgent(String(agent.id))}><span className="item-title"><span className="item-name">{agent.name || agent.id}</span><span className={classNames('badge', agent.id === activeAgentID && 'ok')}>{agent.id === activeAgentID ? '当前' : 'Agent'}</span></span><span className="item-meta">{agent.id} · Persona {String(agent.persona_key || '')}</span></button>)}
      </ListPane>
      <section className="detail-pane">
        <form className="section" id="agent-form" onSubmit={submitAgent}>
          <div className="hero"><div><h2 id="agent-title">{agentDraft.name || agentDraft.id || '新配置'}</h2><div className="meta" id="agent-meta">当前：{activeAgentID || '无'}</div></div><div className="actions"><button className="btn good" id="activate-agent" type="button" disabled={!selectedAgent || selectedAgent === activeAgentID} onClick={activateSelectedAgent}>设为当前</button><button className="btn primary" id="save-agent" type="submit">保存配置</button></div></div>
          <div className="grid">
            <Field id="a-id" label="ID" value={String(agentDraft.id || '')} onChange={value => patchAgentDraft('id', value)} readOnly={!!selectedAgent} mono />
            <Field id="a-name" label="名称" value={String(agentDraft.name || '')} onChange={value => patchAgentDraft('name', value)} />
            <div className="field"><label htmlFor="a-persona">Persona</label><select id="a-persona" value={String(agentDraft.persona_key || '')} onChange={event => patchAgentDraft('persona_key', event.target.value)}>{personas.map(persona => <option key={persona.key} value={persona.key}>{persona.name || persona.key}</option>)}</select></div>
            <Field id="ctx-input-budget" type="number" label="输入预算 Token" value={String(field(field(agentDraft, 'context_overrides', {}), 'input_budget_tokens', ''))} onChange={value => updateAgentPath(['context_overrides', 'input_budget_tokens'], toInt(value))} />
            <Field id="ctx-reserve-output" type="number" label="预留输出 Token" value={String(field(field(agentDraft, 'context_overrides', {}), 'reserve_output_tokens', ''))} onChange={value => updateAgentPath(['context_overrides', 'reserve_output_tokens'], toInt(value))} />
          </div>
          <datalist id="model-options">{modelOptions.map(model => <option key={model} value={model} />)}</datalist>
          <div className="slot-grid" id="slot-grid">
            {slotDefs.map(([id, label]) => <SlotEditor key={id} id={id} label={label} draft={agentDraft} providers={providers} providerPresets={providerPresets} models={modelOptions} onDraft={replaceAgentDraft} />)}
          </div>
          <div className="actions foot"><button className="btn danger" id="delete-agent" type="button" disabled={!selectedAgent} onClick={deleteSelectedAgent}>删除</button></div>
        </form>
      </section>
    </div>
  );
});

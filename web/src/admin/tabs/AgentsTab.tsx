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
      <ListPane title="Agent Configs" count={`${agents.length} configs · active: ${activeAgentID || 'none'}`} searchID="agent-search" searchValue={query} onSearch={setQuery} onNew={newAgent} onReload={reloadAgents}>
        {visibleAgents.map(agent => <button className={classNames('item', selectedAgent === agent.id && 'active')} type="button" key={agent.id} onClick={() => selectAgent(String(agent.id))}><span className="item-title"><span className="item-name">{agent.name || agent.id}</span><span className={classNames('badge', agent.id === activeAgentID && 'ok')}>{agent.id === activeAgentID ? 'active' : 'agent'}</span></span><span className="item-meta">{agent.id} · persona {String(agent.persona_key || '')}</span></button>)}
      </ListPane>
      <section className="detail-pane">
        <form className="section" id="agent-form" onSubmit={submitAgent}>
          <div className="hero"><div><h2 id="agent-title">{agentDraft.name || agentDraft.id || 'New Config'}</h2><div className="meta" id="agent-meta">Active: {activeAgentID || 'none'}</div></div><div className="actions"><button className="btn good" id="activate-agent" type="button" disabled={!selectedAgent || selectedAgent === activeAgentID} onClick={activateSelectedAgent}>Activate</button><button className="btn primary" id="save-agent" type="submit">Save Config</button></div></div>
          <div className="grid">
            <Field id="a-id" label="ID" value={String(agentDraft.id || '')} onChange={value => patchAgentDraft('id', value)} readOnly={!!selectedAgent} mono />
            <Field id="a-name" label="Name" value={String(agentDraft.name || '')} onChange={value => patchAgentDraft('name', value)} />
            <div className="field"><label htmlFor="a-persona">Persona</label><select id="a-persona" value={String(agentDraft.persona_key || '')} onChange={event => patchAgentDraft('persona_key', event.target.value)}>{personas.map(persona => <option key={persona.key} value={persona.key}>{persona.name || persona.key}</option>)}</select></div>
            <Field id="ctx-input-budget" type="number" label="Input Budget Tokens" value={String(field(field(agentDraft, 'context_overrides', {}), 'input_budget_tokens', ''))} onChange={value => updateAgentPath(['context_overrides', 'input_budget_tokens'], toInt(value))} />
            <Field id="ctx-reserve-output" type="number" label="Reserve Output Tokens" value={String(field(field(agentDraft, 'context_overrides', {}), 'reserve_output_tokens', ''))} onChange={value => updateAgentPath(['context_overrides', 'reserve_output_tokens'], toInt(value))} />
          </div>
          <datalist id="model-options">{modelOptions.map(model => <option key={model} value={model} />)}</datalist>
          <div className="slot-grid" id="slot-grid">
            {slotDefs.map(([id, label]) => <SlotEditor key={id} id={id} label={label} draft={agentDraft} providers={providers} providerPresets={providerPresets} models={modelOptions} onDraft={replaceAgentDraft} />)}
          </div>
          <div className="actions foot"><button className="btn danger" id="delete-agent" type="button" disabled={!selectedAgent} onClick={deleteSelectedAgent}>Delete</button></div>
        </form>
      </section>
    </div>
  );
});

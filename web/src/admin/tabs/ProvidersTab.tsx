import { memo, useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { boolField, field } from '../../shared/lib/data';
import type { ProviderAdmin } from '../hooks/useProviderAdmin';
import { matchesQuery } from '../lib/adminData';
import { Field } from '../components/Field';
import { ListPane } from '../components/ListPane';

export type ProvidersTabProps = Pick<ProviderAdmin,
  'providerPresets' |
  'providers' |
  'providerModels' |
  'providerEnv' |
  'selectedProvider' |
  'providerDraft' |
  'reloadProviders' |
  'selectProvider' |
  'patchProviderDraft' |
  'setProviderCapability' |
  'applyProviderPreset' |
  'newProvider' |
  'submitProvider' |
  'refreshSelectedProviderModels' |
  'testSelectedProvider' |
  'deleteSelectedProvider'
>;

export default memo(function ProvidersTab({
  providerPresets,
  providers,
  providerModels,
  providerEnv,
  selectedProvider,
  providerDraft,
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
}: ProvidersTabProps) {
  const [query, setQuery] = useState('');
  const capabilities = new Set(Array.isArray(providerDraft.capabilities) ? providerDraft.capabilities : ['chat']);
  const visibleProviders = providers.filter(provider => matchesQuery(query, provider.id, provider.name, provider.preset_id, provider.protocol, provider.base_url));
  const models = selectedProvider ? providerModels[selectedProvider] || [] : [];

  return (
    <div className="admin-split">
      <ListPane title="Providers" count={`${providers.length} providers`} searchID="provider-search" searchValue={query} onSearch={setQuery} onNew={newProvider} onReload={reloadProviders}>
        {visibleProviders.map(provider => <button className={classNames('item', selectedProvider === provider.id && 'active')} type="button" key={provider.id} onClick={() => selectProvider(String(provider.id))}><span className="item-title"><span className="item-name">{provider.name || provider.id}</span><span className="badge">{String(provider.preset_id || provider.protocol || 'provider')}</span></span><span className="item-meta">{String(provider.base_url || '')}</span></button>)}
      </ListPane>
      <section className="detail-pane">
        <form className="section" id="provider-form" onSubmit={submitProvider}>
          <div className="hero"><div><h2 id="provider-title">{providerDraft.name || providerDraft.id || 'New Provider'}</h2><div className="meta" id="provider-meta">{String(providerDraft.protocol || 'openai_compatible')} / {String(providerDraft.model_discovery || 'manual')}</div></div><div className="actions"><button className="btn ghost" id="test-provider" type="button" disabled={!selectedProvider} onClick={testSelectedProvider}>Test</button><button className="btn ghost" id="refresh-models" type="button" disabled={!selectedProvider} onClick={refreshSelectedProviderModels}>Refresh Models</button><button className="btn primary" id="save-provider" type="submit">Save Provider</button></div></div>
          <div className="grid">
            <Field id="p-id" label="ID" value={String(providerDraft.id || '')} onChange={value => patchProviderDraft('id', value)} readOnly={!!selectedProvider} mono />
            <Field id="p-name" label="Name" value={String(providerDraft.name || '')} onChange={value => patchProviderDraft('name', value)} />
            <div className="field"><label htmlFor="p-preset">Preset</label><select id="p-preset" value={String(providerDraft.preset_id || '')} onChange={event => applyProviderPreset(event.target.value)}><option value="">manual</option>{providerPresets.map(preset => <option key={preset.id} value={preset.id}>{preset.name || preset.id}</option>)}</select></div>
            <div className="field"><label htmlFor="p-protocol">Protocol</label><select id="p-protocol" value={String(providerDraft.protocol || 'openai_compatible')} onChange={event => patchProviderDraft('protocol', event.target.value)}><option value="openai_compatible">openai_compatible</option><option value="anthropic">anthropic</option></select></div>
            <div className="field"><label htmlFor="p-discovery">Model Discovery</label><select id="p-discovery" value={String(providerDraft.model_discovery || 'manual')} onChange={event => patchProviderDraft('model_discovery', event.target.value)}><option value="manual">manual</option><option value="openai_models">openai_models</option><option value="anthropic_models">anthropic_models</option></select></div>
            <Field id="p-base-url" label="Base URL" value={String(providerDraft.base_url || '')} onChange={value => patchProviderDraft('base_url', value)} mono />
            <Field id="p-api-key-env" label="API Key Env" value={String(providerDraft.api_key_env || '')} onChange={value => patchProviderDraft('api_key_env', value)} mono />
            <label className="check"><input id="p-enabled" type="checkbox" checked={boolField(providerDraft, 'enabled')} onChange={event => patchProviderDraft('enabled', event.target.checked)} /> Enabled</label>
            <label className="check"><input id="p-cap-chat" type="checkbox" checked={capabilities.has('chat')} onChange={event => setProviderCapability('chat', event.target.checked)} /> Chat</label>
            <label className="check"><input id="p-cap-embedding" type="checkbox" checked={capabilities.has('embedding')} onChange={event => setProviderCapability('embedding', event.target.checked)} /> Embedding</label>
            <label className="check"><input id="p-cap-rerank" type="checkbox" checked={capabilities.has('rerank')} onChange={event => setProviderCapability('rerank', event.target.checked)} /> Rerank</label>
            <div className="field"><label>Environment</label><span className="badge" id="provider-env-status">{String(field(providerEnv, 'status', field(providerEnv, 'state', selectedProvider ? 'checked' : 'not checked')))}</span></div>
          </div>
          <div className="actions foot"><button className="btn danger" id="delete-provider" type="button" disabled={!selectedProvider} onClick={deleteSelectedProvider}>Delete</button></div>
        </form>
        <div className="section"><h3>Models</h3><div className="models" id="provider-models">{models.map(model => <span className="badge" key={String(model.id || model.name)}>{String(model.id || model.name)}</span>)}</div></div>
      </section>
    </div>
  );
});

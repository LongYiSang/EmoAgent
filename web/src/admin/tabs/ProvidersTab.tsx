import { memo, useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import type { AnyRecord } from '../../shared/lib/api';
import { arrayField, boolField, field, isRecord, stringField } from '../../shared/lib/data';
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
      <ListPane title="模型服务" count={`${providers.length} 个 Provider`} searchID="provider-search" searchValue={query} searchLabel="Provider" onSearch={setQuery} onNew={newProvider} onReload={reloadProviders}>
        {visibleProviders.map(provider => <button className={classNames('item', selectedProvider === provider.id && 'active')} type="button" key={provider.id} onClick={() => selectProvider(String(provider.id))}><span className="item-title"><span className="item-name">{provider.name || provider.id}</span><span className="badge">{String(provider.preset_id || provider.protocol || 'provider')}</span></span><span className="item-meta">{String(provider.base_url || '')}</span></button>)}
      </ListPane>
      <section className="detail-pane">
        <form className="section" id="provider-form" onSubmit={submitProvider}>
          <div className="hero"><div><h2 id="provider-title">{providerDraft.name || providerDraft.id || '新 Provider'}</h2><div className="meta" id="provider-meta">{String(providerDraft.protocol || 'openai_compatible')} / {String(providerDraft.model_discovery || 'manual')}</div></div><div className="actions"><button className="btn ghost" id="test-provider" type="button" disabled={!selectedProvider} onClick={testSelectedProvider}>测试</button><button className="btn ghost" id="refresh-models" type="button" disabled={!selectedProvider} onClick={refreshSelectedProviderModels}>刷新模型</button><button className="btn primary" id="save-provider" type="submit">保存 Provider</button></div></div>
          <div className="grid">
            <Field id="p-id" label="ID" value={String(providerDraft.id || '')} onChange={value => patchProviderDraft('id', value)} readOnly={!!selectedProvider} mono />
            <Field id="p-name" label="名称" value={String(providerDraft.name || '')} onChange={value => patchProviderDraft('name', value)} />
            <div className="field"><label htmlFor="p-preset">预设</label><select id="p-preset" value={String(providerDraft.preset_id || '')} onChange={event => applyProviderPreset(event.target.value)}><option value="">manual</option>{providerPresets.map(preset => <option key={preset.id} value={preset.id}>{preset.name || preset.id}</option>)}</select></div>
            <div className="field"><label htmlFor="p-protocol">协议</label><select id="p-protocol" value={String(providerDraft.protocol || 'openai_compatible')} onChange={event => patchProviderDraft('protocol', event.target.value)}><option value="openai_compatible">openai_compatible</option><option value="anthropic">anthropic</option></select></div>
            <div className="field"><label htmlFor="p-discovery">模型发现</label><select id="p-discovery" value={String(providerDraft.model_discovery || 'manual')} onChange={event => patchProviderDraft('model_discovery', event.target.value)}><option value="manual">manual</option><option value="openai_models">openai_models</option><option value="anthropic_models">anthropic_models</option><option value="siliconflow_models">siliconflow_models</option></select></div>
            <Field id="p-base-url" label="Base URL" value={String(providerDraft.base_url || '')} onChange={value => patchProviderDraft('base_url', value)} mono />
            <Field id="p-api-key-env" label="API Key 环境变量" value={String(providerDraft.api_key_env || '')} onChange={value => patchProviderDraft('api_key_env', value)} mono />
            <label className="check"><input id="p-enabled" type="checkbox" checked={boolField(providerDraft, 'enabled')} onChange={event => patchProviderDraft('enabled', event.target.checked)} /> 启用</label>
            <label className="check"><input id="p-cap-chat" type="checkbox" checked={capabilities.has('chat')} onChange={event => setProviderCapability('chat', event.target.checked)} /> Chat</label>
            <label className="check"><input id="p-cap-embedding" type="checkbox" checked={capabilities.has('embedding')} onChange={event => setProviderCapability('embedding', event.target.checked)} /> Embedding</label>
            <label className="check"><input id="p-cap-rerank" type="checkbox" checked={capabilities.has('rerank')} onChange={event => setProviderCapability('rerank', event.target.checked)} /> Rerank</label>
            <div className="field"><label>环境状态</label><span className="badge" id="provider-env-status">{String(field(providerEnv, 'status', field(providerEnv, 'state', selectedProvider ? 'checked' : 'not checked')))}</span></div>
          </div>
          <div className="actions foot"><button className="btn danger" id="delete-provider" type="button" disabled={!selectedProvider} onClick={deleteSelectedProvider}>删除</button></div>
        </form>
        <div className="section"><h3>模型</h3><div className="models" id="provider-models">{models.map(model => {
          const name = modelName(model);
          const badges = modelCapabilityBadges(model);
          return <span className={classNames('model-chip', modelSupportsImage(model) && 'vision')} key={name}><span className="model-name">{name}</span>{badges.map(badge => <span className="badge model-capability" key={badge}>{badge}</span>)}</span>;
        })}</div></div>
      </section>
    </div>
  );
});

function modelName(model: AnyRecord): string {
  return stringField(model, 'id') || stringField(model, 'name') || 'unknown-model';
}

function modelSupportsImage(model: AnyRecord): boolean {
  return capabilityInputModalities(model).includes('image') && capabilityStringArray(model, 'image_transports').length > 0;
}

function modelCapabilityBadges(model: AnyRecord): string[] {
  const input = capabilityInputModalities(model);
  const transports = capabilityStringArray(model, 'image_transports');
  const formats = capabilityStringArray(model, 'image_formats').map(format => format.replace(/^image\//, ''));
  const source = capabilityString(model, 'capability_source');
  const confidence = Number(capabilityField(model, 'confidence', 0));
  const badges: string[] = [];

  if (input.includes('image') && transports.length) badges.push('vision');
  else if (input.includes('image')) badges.push('image/no transport');
  else if (input.includes('text')) badges.push('text');
  if (stringField(model, 'sub_type')) badges.push(stringField(model, 'sub_type'));
  if (transports.length) badges.push(transports.slice(0, 2).join('/'));
  if (formats.length) badges.push(formats.slice(0, 2).join('/'));
  if (source) badges.push(capabilitySourceLabel(source));
  if (confidence > 0) badges.push(`${Math.round(confidence * 100)}%`);
  return badges;
}

function capabilityInputModalities(model: AnyRecord): string[] {
  return capabilityStringArray(model, 'input_modalities');
}

function capabilityStringArray(model: AnyRecord, key: string): string[] {
  const cap = field<unknown>(model, 'capabilities', {});
  const source = isRecord(cap) ? cap : model;
  return arrayField<unknown>(source, key).map(value => String(value)).filter(Boolean);
}

function capabilityString(model: AnyRecord, key: string): string {
  const value = capabilityField(model, key, '');
  return typeof value === 'string' ? value : value == null ? '' : String(value);
}

function capabilityField<T>(model: AnyRecord, key: string, fallback: T): T | unknown {
  const cap = field<unknown>(model, 'capabilities', {});
  if (isRecord(cap)) {
    return field<unknown>(cap, key, fallback);
  }
  return field<unknown>(model, key, fallback);
}

function capabilitySourceLabel(source: string): string {
  switch (source) {
    case 'manual_override':
      return 'manual';
    case 'provider_metadata':
      return 'metadata';
    case 'provider_docs_preset':
      return 'preset';
    case 'probe_passed':
      return 'probe';
    case 'probe_failed':
      return 'probe failed';
    case 'merged':
      return 'merged';
    default:
      return source;
  }
}

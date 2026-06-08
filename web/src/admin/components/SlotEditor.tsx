import { useEffect, useState } from 'react';

import { classNames } from '../../shared/lib/classNames';
import { field, isRecord, pretty, toFloat, toInt } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import type { AgentConfig, Provider, ProviderPreset } from '../protocol/adminApi';
import {
  cleanDeep,
  cloneRecord,
  hasSlotParamValue,
  parseJSONRecord,
  providerPresetForBinding,
  recommendedParamValue,
  setNested,
  slotDefaults,
  slotParamMeta,
  slotParams,
  type SlotParam,
  writeSlotParam,
} from '../lib/adminData';
import { Field } from './Field';

export function SlotEditor({ id, label, draft, providers, providerPresets, models, onDraft }: { id: string; label: string; draft: AgentConfig; providers: Provider[]; providerPresets: ProviderPreset[]; models: string[]; onDraft: (draft: AgentConfig) => void }) {
  const [group, name] = id.split('-');
  const binding = field<AnyRecord>(field(draft, group, {}), name, {});
  const params = field<AnyRecord>(binding, 'params', {});
  const extra = isRecord(params.extra) ? params.extra : {};
  const formattedExtra = pretty(extra);
  const [extraDraft, setExtraDraft] = useState(formattedExtra);
  const [extraError, setExtraError] = useState('');
  const preset = providerPresetForBinding(providers, providerPresets, String(binding.provider_id || ''));
  useEffect(() => {
    setExtraDraft(formattedExtra);
    setExtraError('');
  }, [formattedExtra, id]);

  function setBinding(path: string[], value: unknown) {
    setNested(draft, onDraft, [group, name, ...path], value);
  }
  function commitExtraDraft() {
    try {
      setBinding(['params', 'extra'], parseJSONRecord(extraDraft));
      setExtraError('');
    } catch (error) {
      setExtraError(error instanceof Error ? error.message : 'JSON 格式无效');
    }
  }
  function applyRecommendedParams() {
    if (!preset) return;
    const defaults = slotDefaults(id, preset);
    const next = cloneRecord(draft);
    const [nextGroup, nextName] = id.split('-');
    if (!next[nextGroup] || typeof next[nextGroup] !== 'object' || Array.isArray(next[nextGroup])) next[nextGroup] = {};
    const groupRecord = next[nextGroup] as AnyRecord;
    if (!groupRecord[nextName] || typeof groupRecord[nextName] !== 'object' || Array.isArray(groupRecord[nextName])) groupRecord[nextName] = {};
    const nextBinding = groupRecord[nextName] as AnyRecord;
    if (!nextBinding.params || typeof nextBinding.params !== 'object' || Array.isArray(nextBinding.params)) nextBinding.params = {};
    const nextParams = nextBinding.params as AnyRecord;
    for (const param of slotParams) {
      const value = recommendedParamValue(defaults, param);
      if (value !== undefined && !hasSlotParamValue(nextParams, param)) writeSlotParam(nextParams, param, value);
    }
    onDraft(cleanDeep(next));
  }
  function paramMeta(param: SlotParam) {
    return slotParamMeta(id, params, preset, param);
  }
  function renderNote(param: SlotParam) {
    const meta = paramMeta(param);
    return <div className={classNames('param-note', meta.warn && 'warn')}>{meta.note}</div>;
  }
  return (
    <div className="slot" data-slot={id}>
      <div className="slot-head"><strong>{label}</strong><button className="btn ghost mini" type="button" onClick={applyRecommendedParams}>应用推荐值</button></div>
      <div className="grid compact">
        <div className="field"><label htmlFor={`${id}-provider`}>Provider</label><select id={`${id}-provider`} value={String(binding.provider_id || '')} onChange={event => setBinding(['provider_id'], event.target.value)}><option value="">选择 Provider</option>{providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name || provider.id}</option>)}</select></div>
        <Field id={`${id}-model`} label="Model" value={String(binding.model || '')} onChange={value => setBinding(['model'], value)} list="model-options" />
        <div className={classNames('field', paramMeta('max_tokens').hidden && 'hidden-param')}><label htmlFor={`${id}-max`}>最大 Token 数</label><input id={`${id}-max`} type="number" value={String(params.max_tokens ?? '')} onChange={event => setBinding(['params', 'max_tokens'], toInt(event.target.value))} />{renderNote('max_tokens')}</div>
        <div className={classNames('field', paramMeta('temperature').hidden && 'hidden-param')}><label htmlFor={`${id}-temp`}>Temperature</label><input id={`${id}-temp`} type="number" value={String(params.temperature ?? '')} onChange={event => setBinding(['params', 'temperature'], toFloat(event.target.value))} />{renderNote('temperature')}</div>
        <div className={classNames('field', paramMeta('stream').hidden && 'hidden-param')}><label htmlFor={`${id}-stream`}>流式输出</label><select id={`${id}-stream`} value={params.stream === undefined ? '' : String(params.stream)} onChange={event => setBinding(['params', 'stream'], event.target.value === '' ? undefined : event.target.value === 'true')}><option value="">继承</option><option value="true">true</option><option value="false">false</option></select>{renderNote('stream')}</div>
        <div className={classNames('field', paramMeta('top_p').hidden && 'hidden-param')}><label htmlFor={`${id}-top-p`}>Top P</label><input id={`${id}-top-p`} type="number" value={String(params.top_p ?? '')} onChange={event => setBinding(['params', 'top_p'], toFloat(event.target.value))} />{renderNote('top_p')}</div>
        <div className={classNames('field', paramMeta('presence_penalty').hidden && 'hidden-param')}><label htmlFor={`${id}-presence`}>Presence Penalty</label><input id={`${id}-presence`} type="number" value={String(params.presence_penalty ?? '')} onChange={event => setBinding(['params', 'presence_penalty'], toFloat(event.target.value))} />{renderNote('presence_penalty')}</div>
        <div className={classNames('field', paramMeta('frequency_penalty').hidden && 'hidden-param')}><label htmlFor={`${id}-frequency`}>Frequency Penalty</label><input id={`${id}-frequency`} type="number" value={String(params.frequency_penalty ?? '')} onChange={event => setBinding(['params', 'frequency_penalty'], toFloat(event.target.value))} />{renderNote('frequency_penalty')}</div>
        <div className={classNames('field', paramMeta('reasoning_effort').hidden && 'hidden-param')}><label htmlFor={`${id}-reasoning`}>Reasoning 强度</label><input id={`${id}-reasoning`} value={String(params.reasoning_effort ?? '')} onChange={event => setBinding(['params', 'reasoning_effort'], event.target.value)} />{renderNote('reasoning_effort')}</div>
        <div className={classNames('field', paramMeta('thinking_mode').hidden && 'hidden-param')}><label htmlFor={`${id}-thinking-mode`}>Thinking 模式</label><input id={`${id}-thinking-mode`} value={String(field(field(params, 'thinking', {}), 'mode', ''))} onChange={event => setBinding(['params', 'thinking', 'mode'], event.target.value)} />{renderNote('thinking_mode')}</div>
        <div className={classNames('field', paramMeta('thinking_budget').hidden && 'hidden-param')}><label htmlFor={`${id}-thinking-budget`}>Thinking 预算</label><input id={`${id}-thinking-budget`} type="number" value={String(field(field(params, 'thinking', {}), 'budget_tokens', ''))} onChange={event => setBinding(['params', 'thinking', 'budget_tokens'], toInt(event.target.value))} />{renderNote('thinking_budget')}</div>
        <div className={classNames('field', paramMeta('thinking_effort').hidden && 'hidden-param')}><label htmlFor={`${id}-thinking-effort`}>Thinking 强度</label><input id={`${id}-thinking-effort`} value={String(field(field(params, 'thinking', {}), 'effort', ''))} onChange={event => setBinding(['params', 'thinking', 'effort'], event.target.value)} />{renderNote('thinking_effort')}</div>
      </div>
      <div className={classNames('field', paramMeta('extra').hidden && 'hidden-param')}><label htmlFor={`${id}-extra`}>额外 JSON</label><textarea id={`${id}-extra`} value={extraDraft} onChange={event => { setExtraDraft(event.target.value); setExtraError(''); }} onBlur={commitExtraDraft} />{extraError && <div className="field-error">{extraError}</div>}{renderNote('extra')}</div>
      <div hidden>{models.join(',')}</div>
    </div>
  );
}

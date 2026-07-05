import { memo } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { arrayField, boolField, field, stringField, toInt } from '../../shared/lib/data';
import { classNames } from '../../shared/lib/classNames';
import type { PlatformAdmin } from '../hooks/usePlatformAdmin';
import type { AgentConfig } from '../protocol/adminApi';
import { Field } from '../components/Field';

export type PlatformsTabProps = PlatformAdmin & { agents: AgentConfig[] };

export default memo(function PlatformsTab({
  platformDraft,
  platformStatus,
  platformIssues,
  platformJSON,
  adapterID,
  reloadPlatformAdmin,
  updatePlatformPath,
  savePlatformDraft,
  testPlatformConnection,
  copySnowLumaConfig,
  copyEmoAgentYAML,
  agents,
}: PlatformsTabProps) {
  const common = field<AnyRecord>(platformDraft, 'common', {});
  const adapter = field<AnyRecord>(field<AnyRecord>(platformDraft, 'adapters', {}), adapterID, {});
  const config = field<AnyRecord>(adapter, 'config', {});
  const transport = field<AnyRecord>(config, 'transport', {});
  const routing = field<AnyRecord>(config, 'routing', {});
  const message = field<AnyRecord>(config, 'message', {});
  const outbound = field<AnyRecord>(config, 'outbound', {});
  const statusAdapter = arrayField<AnyRecord>(platformStatus, 'adapters').find(item => stringField(item, 'id') === adapterID) || {};
  const statusTransport = field<AnyRecord>(statusAdapter, 'transport', {});
  const auth = field<AnyRecord>(statusAdapter, 'auth', {});
  const connected = boolField(statusTransport, 'connected');
  const enabled = boolField(platformDraft, 'enabled') && boolField(adapter, 'enabled');
  const defaultAgentID = stringField(common, 'default_agent_id');
  const boundAgent = agents.find(agent => stringField(agent, 'id') === defaultAgentID);

  return (
    <div className="section">
      <div className="hero">
        <div>
          <h2>消息平台</h2>
          <div className="meta">SnowLuma / OneBot v11 · {adapterID}</div>
        </div>
        <div className="actions">
          <button className="btn ghost" type="button" onClick={reloadPlatformAdmin}>重新加载</button>
          <button className="btn ghost" type="button" onClick={testPlatformConnection}>测试连接</button>
          <button className="btn primary" type="button" onClick={savePlatformDraft}>保存配置</button>
        </div>
      </div>

      <div className="grid compact">
        <div className="field">
          <label>配置状态</label>
          <span className={classNames('badge', enabled ? 'ok' : 'warn')}>{enabled ? '启用' : '禁用'}</span>
        </div>
        <div className="field">
          <label>连接状态</label>
          <span className={classNames('badge', connected ? 'ok' : 'warn')}>{connected ? '已连接' : statusLabel(statusTransport)}</span>
        </div>
        <div className="field">
          <label>Self ID</label>
          <span className="mono">{stringField(statusTransport, 'self_id') || '-'}</span>
        </div>
        <div className="field">
          <label>Token</label>
          <span className={classNames('badge', boolField(auth, 'access_token_configured') ? 'ok' : 'warn')}>{boolField(auth, 'access_token_configured') ? 'env configured' : 'missing env'}</span>
        </div>
        <div className="field">
          <label>绑定 Agent</label>
          <span className="mono">{boundAgentLabel(boundAgent, defaultAgentID)}</span>
        </div>
      </div>

      <div className="slot" style={{ marginTop: 16 }}>
        <div className="slot-head"><strong>基础配置</strong><span className="badge">{adapterID}</span></div>
        <div className="grid compact">
          <label className="check"><input type="checkbox" checked={boolField(platformDraft, 'enabled')} onChange={event => updatePlatformPath(['enabled'], event.target.checked)} /> 启用 platforms</label>
          <label className="check"><input type="checkbox" checked={boolField(adapter, 'enabled')} onChange={event => updatePlatformPath(['adapters', adapterID, 'enabled'], event.target.checked)} /> 启用 qq-main</label>
          <div className="field">
            <label htmlFor="platform-agent-id">Agent 配置</label>
            <select id="platform-agent-id" value={defaultAgentID} onChange={event => updatePlatformPath(['common', 'default_agent_id'], event.target.value)}>
              <option value="">未绑定，使用兼容逻辑</option>
              {agents.map(agent => (
                <option key={stringField(agent, 'id')} value={stringField(agent, 'id')}>
                  {agentOptionLabel(agent)}
                </option>
              ))}
            </select>
          </div>
          <Field id="platform-id" label="platform_id" value={stringField(adapter, 'platform_id') || 'qq'} onChange={value => updatePlatformPath(['adapters', adapterID, 'platform_id'], value)} />
          <Field id="platform-reverse-path" label="reverse_path" value={stringField(transport, 'reverse_path')} onChange={value => updatePlatformPath(['adapters', adapterID, 'config', 'transport', 'reverse_path'], value)} mono />
          <Field id="platform-token-env" label="access_token_env" value={stringField(transport, 'access_token_env')} onChange={value => updatePlatformPath(['adapters', adapterID, 'config', 'transport', 'access_token_env'], value)} mono />
        </div>
      </div>

      <div className="slot" style={{ marginTop: 16 }}>
        <div className="slot-head"><strong>路由与消息</strong><span className="badge">private only</span></div>
        <div className="grid compact">
          <label className="check"><input type="checkbox" checked={boolField(routing, 'private_enabled')} onChange={event => updatePlatformPath(['adapters', adapterID, 'config', 'routing', 'private_enabled'], event.target.checked)} /> 私聊</label>
          <label className="check"><input type="checkbox" checked={boolField(routing, 'group_enabled')} onChange={event => updatePlatformPath(['adapters', adapterID, 'config', 'routing', 'group_enabled'], event.target.checked)} /> 群聊</label>
          <NumberField id="platform-max-text" label="max_text_chars" value={field(message, 'max_text_chars', 8000)} min={1} onChange={value => updatePlatformPath(['adapters', adapterID, 'config', 'message', 'max_text_chars'], value)} />
          <NumberField id="platform-max-message" label="max_message_chars" value={field(outbound, 'max_message_chars', 1800)} min={1} onChange={value => updatePlatformPath(['adapters', adapterID, 'config', 'outbound', 'max_message_chars'], value)} />
        </div>
      </div>

      <div className="actions foot">
        <button className="btn ghost" type="button" onClick={copySnowLumaConfig}>复制 SnowLuma 连接信息</button>
        <button className="btn ghost" type="button" onClick={copyEmoAgentYAML}>复制 EmoAgent 配置</button>
      </div>

      <details className="slot" style={{ marginTop: 16 }} open={platformIssues.length > 0}>
        <summary className="slot-head"><strong>诊断</strong><span className={classNames('badge', platformIssues.length > 0 && 'warn')}>{platformIssues.length}</span></summary>
        <div className="timeline-list">
          {platformIssues.length ? platformIssues.map((issue, index) => (
            <div className="timeline-item" key={`${String(field(issue, 'path', 'issue'))}-${index}`}>
              <b>{String(field(issue, 'path', 'platforms'))}<span className={classNames('badge', String(field(issue, 'severity', '')) === 'error' && 'warn')}>{String(field(issue, 'severity', 'info'))}</span></b>
              <span>{String(field(issue, 'message', ''))}</span>
            </div>
          )) : <div className="hint">暂无消息平台配置问题</div>}
        </div>
      </details>

      <details className="slot" style={{ marginTop: 16 }}>
        <summary className="slot-head"><strong>当前草稿</strong><span className="badge">JSON</span></summary>
        <pre className="code">{platformJSON}</pre>
      </details>
    </div>
  );
});

function statusLabel(transport: AnyRecord) {
  const state = stringField(transport, 'state');
  if (state === 'waiting') return '等待连接';
  if (state === 'stopped') return '已停止';
  return state || '未连接';
}

function NumberField({ id, label, value, min, onChange }: { id: string; label: string; value: unknown; min: number; onChange: (value: number | undefined) => void }) {
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input id={id} type="number" min={min} value={String(value ?? '')} onChange={event => onChange(toInt(event.target.value))} />
    </div>
  );
}

function agentOptionLabel(agent: AgentConfig) {
  const id = stringField(agent, 'id');
  const name = stringField(agent, 'name');
  const persona = stringField(agent, 'persona_key') || 'default';
  return [id, name, `Persona ${persona}`].filter(Boolean).join(' · ');
}

function boundAgentLabel(agent: AgentConfig | undefined, defaultAgentID: string) {
  if (!defaultAgentID) return '未绑定';
  if (!agent) return `${defaultAgentID} · 未找到`;
  return agentOptionLabel(agent);
}

import { memo, useEffect, useMemo, useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import type { AgentConfig } from '../protocol/adminApi';
import type { PromptCenterAdmin } from '../hooks/usePromptCenterAdmin';
import { matchesQuery } from '../lib/adminData';
import { ListPane } from '../components/ListPane';

export type PromptCenterTabProps = PromptCenterAdmin & {
  agents: AgentConfig[];
  activeAgentID: string;
};

function sourceLabel(source: string) {
  const labels: Record<string, string> = {
    embedded_default: '内置默认',
    global_override: '全局覆盖',
    agent_override: 'Agent 自定义',
    agent_default: 'Agent 内置默认',
    persona: 'Persona',
    runtime_dynamic: '运行时',
    pending_work_dynamic: 'Pending Work',
    memory_dynamic: 'Memory',
    agent_affect_dynamic: 'Agent Affect',
    extra_system_dynamic: 'Extra System',
  };
  return labels[source] || source || '-';
}

function copyText(value?: string) {
  if (!value) return;
  void navigator.clipboard?.writeText(value);
}

export default memo(function PromptCenterTab({
  agents,
  activeAgentID,
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
}: PromptCenterTabProps) {
  const [query, setQuery] = useState('');
  const agentID = selectedAgentID || activeAgentID || agents[0]?.id || '';
  const selectedEditable = selectedComponent?.editable ?? false;
  const filtered = useMemo(
    () => visibleComponents.filter(item => matchesQuery(query, item.id, item.name, item.group, item.effective_source)),
    [query, visibleComponents],
  );

  useEffect(() => {
    if (!selectedAgentID && agentID) void setPromptAgentID(agentID);
  }, [agentID, selectedAgentID, setPromptAgentID]);

  return (
    <div className="admin-split">
      <ListPane title="提示词组件" count={`${visibleComponents.length} 个组件`} searchID="prompt-search" searchValue={query} searchLabel="提示词组件" onSearch={setQuery} onReload={() => reloadPromptCenter(agentID)}>
        <label className="check">
          <input type="checkbox" checked={showOverriddenOnly} onChange={event => setShowOverriddenOnly(event.target.checked)} />
          只看有覆盖项
        </label>
        {filtered.map(component => (
          <button className={classNames('item', selectedComponentID === component.id && 'active')} type="button" key={component.id} onClick={() => selectPromptComponent(component.id)}>
            <span className="item-title">
              <span className="item-name">{component.name || component.id}</span>
              <span className={classNames('badge', component.effective_source === 'agent_override' ? 'ok' : component.effective_source === 'agent_default' ? 'warn' : '')}>{sourceLabel(component.effective_source)}</span>
            </span>
            <span className="item-meta">{component.id} · {component.group}</span>
          </button>
        ))}
      </ListPane>

      <section className="detail-pane">
        <div className="section">
          <div className="hero">
            <div>
              <h2>{selectedComponent?.name || '提示词中心'}</h2>
              <div className="meta">{selectedComponent?.id || '未选择'} · effective_source: {sourceLabel(selectedComponent?.effective_source || '')}</div>
            </div>
            <div className="actions">
              <select value={agentID} onChange={event => setPromptAgentID(event.target.value)} aria-label="Agent">
                {agents.map(agent => <option key={agent.id} value={String(agent.id || '')}>{agent.name || agent.id}{agent.id === activeAgentID ? '（当前）' : ''}</option>)}
              </select>
              <button className="btn ghost" type="button" onClick={() => reloadPromptSnapshots(agentID)}>刷新快照</button>
            </div>
          </div>
          {selectedComponent && (
            <div className="kv">
              <span>分组</span><b>{selectedComponent.group}</b>
              <span>风险</span><b>{selectedComponent.risk_level}</b>
              <span>Kind</span><b>{selectedComponent.kind}</b>
              <span>编辑</span><b>{selectedEditable ? '可编辑' : '只读'}</b>
              <span>Hash</span><b className="mono">{selectedComponent.effective_hash}</b>
              <span>Stale</span><b>{selectedComponent.effective_override_stale ? '当前生效覆盖基于旧默认' : '无'}</b>
            </div>
          )}
          {selectedComponent?.description && <p className="meta">{selectedComponent.description}</p>}
          {selectedComponent && !selectedEditable && <p className="meta">此组件当前仅登记默认文本；覆盖保存会在运行时接入后开放。</p>}
          {selectedComponent?.risk_level === 'protocol_sensitive' && (
            <p className="meta">protocol_sensitive：这个提示词会影响工具调用、JSON 输出或 Work 协议。改坏后可能导致任务无法完成。你可以随时恢复默认。</p>
          )}
          {!!lintWarnings.length && (
            <div className="warning-list">
              {lintWarnings.map(warning => <div className="field-error" key={`${warning.component_id}:${warning.code}`}>{warning.severity}: {warning.message}</div>)}
            </div>
          )}
        </div>

        {selectedComponent && (
          <>
            <div className="grid two-col">
              <div className="section nested">
                <div className="row-head"><strong>默认提示词</strong><span className="badge">{selectedComponent.default_hash.slice(0, 10)}</span></div>
                <pre className="code tall">{selectedComponent.default_text}</pre>
              </div>
              <div className="section nested">
                <div className="row-head"><strong>当前 Agent 生效提示词</strong><span className="badge active">{sourceLabel(selectedComponent.effective_source)}</span></div>
                <pre className="code tall">{selectedComponent.effective_text}</pre>
              </div>
            </div>

            <div className="grid two-col">
              <div className="section nested">
                <div className="row-head"><strong>Global 覆盖</strong><span className="badge">{selectedComponent.global_override ? 'custom' : '内置默认'}</span></div>
                <textarea className="tall" value={globalDraft} onChange={event => setGlobalDraft(event.target.value)} spellCheck={false} disabled={!selectedEditable} />
                {selectedComponent.global_override_stale && <div className="field-error">全局覆盖基于旧默认。</div>}
                <div className="actions foot">
                  <button className="btn primary" type="button" disabled={!selectedEditable} onClick={saveGlobalOverride}>保存全局覆盖</button>
                  <button className="btn ghost" type="button" disabled={!selectedEditable} onClick={resetGlobalOverride}>恢复全局为内置默认</button>
                </div>
              </div>
              <div className="section nested">
                <div className="row-head"><strong>当前 Agent 覆盖</strong><span className="badge">{selectedComponent.agent_override?.mode || '继承全局'}</span></div>
                <textarea className="tall" value={agentDraft} onChange={event => setAgentDraft(event.target.value)} spellCheck={false} disabled={!agentID || !selectedEditable} />
                {selectedComponent.agent_override_stale && <div className="field-error">Agent 覆盖基于旧默认。</div>}
                <div className="actions foot">
                  <button className="btn primary" type="button" disabled={!agentID || !selectedEditable} onClick={saveAgentOverride}>保存为此 Agent 自定义</button>
                  <button className="btn ghost" type="button" disabled={!agentID || !selectedEditable} onClick={inheritGlobal}>此 Agent 继承全局设置</button>
                  <button className="btn good" type="button" disabled={!agentID || !selectedEditable} onClick={useEmbeddedDefault}>此 Agent 使用内置默认（忽略全局覆盖）</button>
                </div>
              </div>
            </div>

            <div className="section nested">
              <div className="row-head">
                <strong>预览 effective prompt</strong>
                <div className="actions">
                  <button className="btn ghost" type="button" disabled={!preview?.final_hash} onClick={() => copyText(preview?.final_hash)}>复制 hash</button>
                  <button className="btn ghost" type="button" disabled={!preview?.rendered_text} onClick={() => copyText(preview?.rendered_text)}>复制 prompt</button>
                  <button className="btn primary" type="button" onClick={previewEffectivePrompt}>预览 effective prompt</button>
                </div>
              </div>
              {!!preview?.warnings?.length && (
                <div className="warning-list">
                  {preview.warnings.map(warning => <div className="field-error" key={`${warning.code}:${warning.message}`}>{warning.severity}: {warning.message}</div>)}
                </div>
              )}
              <pre className="code">{preview?.rendered_text || selectedComponent.effective_text}</pre>
              {!!preview?.components?.length && (
                <div className="component-table" aria-label="预览组件">
                  <div className="component-row head"><span>Component</span><span>Source</span><span>Hash</span><span>Length</span></div>
                  {preview.components.map(component => (
                    <div className="component-row" key={`${component.component_id}:${component.source}:${component.scope_id || ''}`}>
                      <span className="mono">{component.component_id}</span>
                      <span>{sourceLabel(component.source)}{component.dynamic ? ' · dynamic' : ''}</span>
                      <span className="mono">{component.effective_hash}</span>
                      <span>{component.text_length ?? '-'}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>

            <div className="section nested">
              <div className="row-head">
                <strong>完整 Emotion prompt 预览</strong>
                <button className="btn primary" type="button" disabled={!agentID} onClick={previewFullEmotionPrompt}>预览完整 Emotion prompt</button>
              </div>
              <div className="grid compact">
                <label className="field">
                  <span className="label">Session ID</span>
                  <input value={fullPreviewSessionID} onChange={event => setFullPreviewSessionID(event.target.value)} placeholder="可选" />
                </label>
                <label className="field">
                  <span className="label">User Message</span>
                  <textarea value={fullPreviewUserMessage} onChange={event => setFullPreviewUserMessage(event.target.value)} placeholder="可选，仅用于构造预览上下文" />
                </label>
                <label className="check"><input type="checkbox" checked={includeMemoryPreview} onChange={event => setIncludeMemoryPreview(event.target.checked)} /> 包含 Memory preview</label>
                <label className="check"><input type="checkbox" checked={includeAgentAffectPreview} onChange={event => setIncludeAgentAffectPreview(event.target.checked)} /> 包含 Agent Affect preview</label>
              </div>
            </div>

            <div className="section nested">
              <div className="row-head"><strong>最近真实注入快照</strong><span className="badge">{snapshots.length}</span></div>
              <div className="timeline-list">
                {snapshots.map(snapshot => (
                  <button className={classNames('timeline-item', selectedSnapshotID === snapshot.id && 'active')} type="button" key={snapshot.id} onClick={() => selectPromptSnapshot(snapshot.id)}>
                    <b>{snapshot.purpose} · {snapshot.model || '-'}</b>
                    <span>{snapshot.id} · Agent {snapshot.agent_id || '-'} · {snapshot.created_at || '-'}</span>
                  </button>
                ))}
                {!snapshots.length && <div className="timeline-item"><span>暂无快照</span></div>}
              </div>
              {snapshotDetail && (
                <div className="section nested snapshot-detail">
                  <div className="row-head">
                    <strong>快照详情</strong>
                    <div className="actions">
                      <button className="btn ghost" type="button" onClick={() => copyText(snapshotDetail.final_hash)}>复制 hash</button>
                      <button className="btn ghost" type="button" disabled={!snapshotDetail.rendered_text} onClick={() => copyText(snapshotDetail.rendered_text)}>复制 prompt</button>
                    </div>
                  </div>
                  <div className="kv">
                    <span>request_id</span><b className="mono">{snapshotDetail.request_id || '-'}</b>
                    <span>turn_id</span><b className="mono">{snapshotDetail.turn_id || '-'}</b>
                    <span>session_id</span><b className="mono">{snapshotDetail.session_id || '-'}</b>
                    <span>agent_id</span><b className="mono">{snapshotDetail.agent_id || '-'}</b>
                    <span>persona_key</span><b className="mono">{snapshotDetail.persona_key || '-'}</b>
                    <span>model</span><b>{snapshotDetail.model || '-'}</b>
                    <span>purpose</span><b>{snapshotDetail.purpose || '-'}</b>
                    <span>final_hash</span><b className="mono">{snapshotDetail.final_hash || '-'}</b>
                    <span>truncated</span><b>{snapshotDetail.truncated ? 'true' : 'false'}</b>
                  </div>
                  {snapshotDetail.truncated && <div className="field-error">rendered_text 已按保留策略截断；final_hash 仍基于完整 prompt。</div>}
                  {!snapshotDetail.rendered_text && <div className="field-error">此快照未保存 rendered_text，仅保留 hash 和组件元信息。</div>}
                  {snapshotDetail.rendered_text && <pre className="code tall">{snapshotDetail.rendered_text}</pre>}
                  <div className="component-table" aria-label="快照组件">
                    <div className="component-row head"><span>Component</span><span>Source</span><span>Scope</span><span>Hash</span><span>Length</span><span>Section</span></div>
                    {snapshotDetail.components.map(component => (
                      <div className="component-row" key={`${component.component_id}:${component.source}:${component.scope_id || ''}:${component.section_name || ''}`}>
                        <span className="mono">{component.component_id}</span>
                        <span>{sourceLabel(component.source)}{component.dynamic ? ' · dynamic' : ''}</span>
                        <span>{component.scope_type || '-'}{component.scope_id ? `:${component.scope_id}` : ''}</span>
                        <span className="mono">{component.effective_hash}</span>
                        <span>{component.text_length ?? '-'}</span>
                        <span>{component.section_name || '-'}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </>
        )}
      </section>
    </div>
  );
});

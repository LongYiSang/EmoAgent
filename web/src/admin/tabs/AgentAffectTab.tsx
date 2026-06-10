import { memo } from 'react';
import { arrayField, boolField, field, formatTime, numberField, pretty, stringField, toFloat, toInt } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import type { AgentAffectAdmin } from '../hooks/useAgentAffectAdmin';
import type { Provider } from '../protocol/adminApi';
import { Field } from '../components/Field';

const vectorKeys = ['valence', 'arousal', 'dominance', 'energy', 'warmth', 'concern', 'curiosity', 'playfulness', 'attachment', 'frustration', 'uncertainty'];

type AgentAffectTabProps = AgentAffectAdmin & {
  providers: Provider[];
  modelOptions: string[];
};

export default memo(function AgentAffectTab({
  personaID,
  sessionID,
  configDraft,
  profileDraft,
  currentMood,
  history,
  pluginWrites,
  queueStatus,
  promptPreview,
  debugInput,
  deltaJSON,
  lastResult,
  setPersonaID,
  setSessionID,
  setDebugInput,
  setDeltaJSON,
  reloadAgentAffect,
  updateConfigPath,
  updateProfileBaseline,
  saveConfigDraft,
  saveProfileDraft,
  evaluatePreview,
  submitCommit,
  applyDelta,
  resetMood,
  refreshPromptPreview,
  processQueueOnce,
  clearFailedJobs,
  supersedePendingJobs,
  configJSON,
  resultJSON,
  providers,
  modelOptions,
}: AgentAffectTabProps) {
  const mood = field<AnyRecord>(currentMood, 'mood', {});
  const moodVector = field<AnyRecord>(mood, 'vector', {});
  const evaluator = field<AnyRecord>(configDraft, 'evaluator', {});
  const stateConfig = field<AnyRecord>(configDraft, 'state', {});
  const promptConfig = field<AnyRecord>(configDraft, 'prompt', {});
  const asyncConfig = field<AnyRecord>(configDraft, 'async', {});
  const batchConfig = field<AnyRecord>(asyncConfig, 'batch', {});
  const evaluations = arrayField<AnyRecord>(history, 'evaluations');
  const events = arrayField<AnyRecord>(history, 'events');
  const queueJobs = arrayField<AnyRecord>(queueStatus, 'jobs');
  const queueBatches = arrayField<AnyRecord>(queueStatus, 'batches');
  const latestBatch = field<AnyRecord>(queueStatus, 'latest_batch', {});
  const stateScope = stringField(stateConfig, 'scope') || 'persona';

  return (
    <div className="section">
      <div className="hero">
        <div>
          <h2>Agent Affect</h2>
          <div className="meta">当前 mood、运行时配置、调试写入与审计</div>
        </div>
        <div className="actions">
          <button className="btn ghost" type="button" onClick={reloadAgentAffect}>重新加载</button>
          <button className="btn primary" type="button" onClick={saveConfigDraft}>保存配置</button>
        </div>
      </div>

      {/* Debug Context */}
      <div className="section nested">
        <div className="row-head">
          <strong>调试上下文</strong>
          <span className="param-note">Persona 决定默认 mood owner；Session 作为过滤条件</span>
        </div>
        <div className="grid compact">
          <div className="field">
            <label>Persona ID</label>
            <input value={personaID} onChange={event => setPersonaID(event.target.value)} />
          </div>
          <div className="field">
            <label>{stateScope === 'persona' ? 'Session Filter' : 'Session ID'}</label>
            <input value={sessionID} onChange={event => setSessionID(event.target.value)} />
          </div>
        </div>
      </div>

      {/* 全局开关与模式 */}
      <div className="grid">
        <label className="check"><input type="checkbox" checked={boolField(configDraft, 'enabled')} onChange={event => updateConfigPath(['enabled'], event.target.checked)} /> 启用 Agent Affect</label>
        <label className="check"><input type="checkbox" checked={boolField(configDraft, 'storage_enabled')} onChange={event => updateConfigPath(['storage_enabled'], event.target.checked)} /> SQLite 持久化</label>
        <div className="field">
          <label>Update Mode</label>
          <select value={stringField(configDraft, 'update_mode') || 'async_after_reply'} onChange={event => updateConfigPath(['update_mode'], event.target.value)}>
            <option value="async_after_reply">async_after_reply</option>
            <option value="sync_before_reply">sync_before_reply</option>
          </select>
        </div>
        <div className="field">
          <label>State Scope</label>
          <select value={stateScope} onChange={event => updateConfigPath(['state', 'scope'], event.target.value)}>
            <option value="persona">persona</option>
            <option value="session">session</option>
          </select>
        </div>
        <div className="field">
          <label>Evaluator Mode</label>
          <select value={stringField(field(configDraft, 'evaluator', {}), 'mode') || 'llm'} onChange={event => updateConfigPath(['evaluator', 'mode'], event.target.value)}>
            <option value="llm">llm</option>
            <option value="disabled">disabled</option>
          </select>
        </div>
        <div className="field">
          <label>Context Mode</label>
          <select value={stringField(field(configDraft, 'context', {}), 'mode') || 'raw_window'} onChange={event => updateConfigPath(['context', 'mode'], event.target.value)}>
            <option value="none">none</option>
            <option value="raw_window">raw_window</option>
            <option value="summary_window">summary_window</option>
            <option value="mixed">mixed</option>
          </select>
        </div>
        <label className="check"><input type="checkbox" checked={boolField(field(configDraft, 'context', {}), 'store_raw_inputs')} onChange={event => updateConfigPath(['context', 'store_raw_inputs'], event.target.checked)} /> 保存 raw input_text</label>
        <label className="check"><input type="checkbox" checked={boolField(promptConfig, 'include_mood_block')} onChange={event => updateConfigPath(['prompt', 'include_mood_block'], event.target.checked)} /> 注入 prompt mood block</label>
        <div className="field">
          <label>Prompt Mode</label>
          <select value={stringField(promptConfig, 'mode') || 'natural_summary'} onChange={event => updateConfigPath(['prompt', 'mode'], event.target.value)}>
            <option value="natural_summary">natural_summary</option>
            <option value="numeric_debug">numeric_debug</option>
            <option value="both">both</option>
          </select>
        </div>
        <label className="check"><input type="checkbox" checked={boolField(promptConfig, 'include_numeric_values')} onChange={event => updateConfigPath(['prompt', 'include_numeric_values'], event.target.checked)} /> Prompt numeric debug</label>
        <label className="check"><input type="checkbox" checked={boolField(asyncConfig, 'worker_enabled')} onChange={event => updateConfigPath(['async', 'worker_enabled'], event.target.checked)} /> 后台 worker</label>
        <label className="check"><input type="checkbox" checked={boolField(batchConfig, 'enabled')} onChange={event => updateConfigPath(['async', 'batch', 'enabled'], event.target.checked)} /> 批量合并</label>
        <div className="field"><label>Batch Max Jobs</label><input type="number" min="1" value={String(field(batchConfig, 'max_jobs', 6))} onChange={event => updateConfigPath(['async', 'batch', 'max_jobs'], toInt(event.target.value))} /></div>
      </div>

      <details className="slot" open>
        <summary className="slot-head">
          <strong>Evaluator / LLM 设置</strong>
          <span className="badge">{stringField(evaluator, 'provider_id') || '未选择 Provider'} / {stringField(evaluator, 'model') || '未指定模型'}</span>
        </summary>
        <div className="grid compact">
          <div className="field">
            <label htmlFor="agent-affect-provider">Provider ID</label>
            <select id="agent-affect-provider" value={stringField(evaluator, 'provider_id')} onChange={event => updateConfigPath(['evaluator', 'provider_id'], event.target.value)}>
              <option value="">继承默认 Provider</option>
              {providers.map(provider => <option key={provider.id} value={provider.id}>{provider.name || provider.id}</option>)}
            </select>
          </div>
          <Field id="agent-affect-model" label="Model" value={stringField(evaluator, 'model')} onChange={value => updateConfigPath(['evaluator', 'model'], value)} list="agent-affect-model-options" mono />
          <datalist id="agent-affect-model-options">
            {modelOptions.map(model => <option key={model} value={model} />)}
          </datalist>
          <label className="check"><input type="checkbox" checked={boolField(evaluator, 'thinking_enabled')} onChange={event => updateConfigPath(['evaluator', 'thinking_enabled'], event.target.checked)} /> Thinking</label>
          <div className="field">
            <label htmlFor="agent-affect-reasoning">推理强度</label>
            <select id="agent-affect-reasoning" value={stringField(evaluator, 'reasoning_effort') || 'medium'} onChange={event => updateConfigPath(['evaluator', 'reasoning_effort'], event.target.value)}>
              <option value="">默认</option>
              <option value="minimal">minimal</option>
              <option value="low">low</option>
              <option value="medium">medium</option>
              <option value="high">high</option>
            </select>
          </div>
          <div className="field"><label htmlFor="agent-affect-max-output">Max Output Tokens</label><input id="agent-affect-max-output" type="number" min="0" value={String(field(evaluator, 'max_output_tokens', ''))} onChange={event => updateConfigPath(['evaluator', 'max_output_tokens'], toInt(event.target.value))} /></div>
          <div className="field"><label htmlFor="agent-affect-timeout">Timeout MS</label><input id="agent-affect-timeout" type="number" min="0" value={String(field(evaluator, 'timeout_ms', ''))} onChange={event => updateConfigPath(['evaluator', 'timeout_ms'], toInt(event.target.value))} /></div>
          <div className="field"><label htmlFor="agent-affect-temp">Temperature</label><input id="agent-affect-temp" type="number" min="0" max="2" step="0.01" value={String(field(evaluator, 'temperature', ''))} onChange={event => updateConfigPath(['evaluator', 'temperature'], toFloat(event.target.value))} /></div>
        </div>
        <div className="param-note">这些字段保存到 agent_affect.evaluator；hidden thinking 仍不持久化。</div>
      </details>

      {/* 当前状态 + Profile */}
      <div className="grid two-col">
        <div className="section nested">
          <div className="row-head">
            <h3>当前 Mood（只读）</h3>
            <span className="badge active">{stringField(mood, 'mood_owner_scope') || stateScope}: {stringField(mood, 'mood_owner_id') || '—'}</span>
          </div>
          <div className="kv">
            <span>Enabled</span><b>{String(boolField(currentMood, 'enabled'))}</b>
            <span>当前心情</span><b>{stringField(mood, 'mood_description') || stringField(mood, 'label') || 'baseline'}</b>
            <span>原因</span><b>{stringField(mood, 'mood_reason') || stringField(mood, 'visible_cause_summary') || stringField(mood, 'cause_summary') || '—'}</b>
            <span>Confidence</span><b>{numberField(mood, 'confidence').toFixed(3)}</b>
            <span>Updated</span><b>{formatTime(field(mood, 'updated_at', ''))}</b>
          </div>
          <pre className="code">{stringField(mood, 'prompt_mood_text') || stringField(mood, 'cause_summary') || 'No prompt mood text.'}</pre>
          <details className="slot">
            <summary className="slot-head"><strong>Debug mood vector</strong><span className="badge">{stringField(mood, 'label') || '—'}</span></summary>
            <div className="pill-row">
              {vectorKeys.map(key => <span className="pill" key={key}>{key}: {numberField(moodVector, key).toFixed(3)}</span>)}
            </div>
          </details>
        </div>

        <div className="section nested">
          <div className="row-head">
            <h3>Profile Baseline</h3>
            <button className="btn ghost mini" type="button" onClick={saveProfileDraft}>保存 Profile</button>
          </div>
          <div className="grid compact">
            {vectorKeys.map(key => (
              <div className="field" key={key}>
                <label>{key}</label>
                <input type="number" step="0.01" value={numberField(field(profileDraft, 'baseline', {}), key)} onChange={event => updateProfileBaseline(key, Number(event.target.value))} />
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="section nested">
        <div className="row-head">
          <h3>Queue / Batch</h3>
          <div className="actions">
            <button className="btn ghost mini" type="button" onClick={reloadAgentAffect}>刷新队列</button>
            <button className="btn ghost mini" type="button" onClick={processQueueOnce}>处理一次</button>
            <button className="btn ghost mini" type="button" onClick={clearFailedJobs}>清理 failed</button>
            <button className="btn danger mini" type="button" onClick={supersedePendingJobs}>Supersede pending</button>
          </div>
        </div>
        <div className="kv">
          <span>Pending</span><b>{numberField(queueStatus, 'pending_jobs')}</b>
          <span>Running</span><b>{numberField(queueStatus, 'running_jobs')}</b>
          <span>Failed</span><b>{numberField(queueStatus, 'failed_jobs')}</b>
          <span>Latest batch</span><b>{stringField(latestBatch, 'id') || '—'}</b>
          <span>Batch job count</span><b>{numberField(latestBatch, 'job_count')}</b>
          <span>Last worker error</span><b>{stringField(latestBatch, 'error_message') || '—'}</b>
        </div>
        <div className="grid two-col">
          <div className="timeline-list">
            {queueJobs.slice(0, 8).map(job => (
              <div className="timeline-item" key={stringField(job, 'id')}>
                <b>{stringField(job, 'status')} / {stringField(job, 'job_type')}</b>
                <span>{stringField(job, 'mood_owner_id')} / {stringField(job, 'turn_id') || '-'}</span>
                <span>{formatTime(field(job, 'created_at', ''))}</span>
              </div>
            ))}
          </div>
          <div className="timeline-list">
            {queueBatches.slice(0, 8).map(batch => (
              <div className="timeline-item" key={stringField(batch, 'id')}>
                <b>{stringField(batch, 'status')} / jobs={numberField(batch, 'job_count')}</b>
                <span>{stringField(batch, 'mood_owner_id')}</span>
                <span>{formatTime(field(batch, 'started_at', ''))}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* 调试工具 */}
      <div className="section nested">
        <div className="row-head">
          <h3>手动调试工具</h3>
        </div>
        <div className="grid two-col">
          <div>
            <div className="field">
              <label>输入摘要（用于评估）</label>
              <textarea rows={3} value={debugInput} onChange={event => setDebugInput(event.target.value)} />
            </div>
            <div className="actions" style={{ marginTop: 8 }}>
              <button className="btn ghost" type="button" onClick={evaluatePreview}>Evaluate (预览)</button>
              <button className="btn primary" type="button" onClick={submitCommit}>Submit (提交)</button>
              <button className="btn danger" type="button" onClick={resetMood}>Reset Mood</button>
            </div>

            <div className="field" style={{ marginTop: 12 }}>
              <label>Delta JSON（手动增量）</label>
              <textarea rows={4} value={deltaJSON} onChange={event => setDeltaJSON(event.target.value)} />
            </div>
            <div className="actions" style={{ marginTop: 6 }}>
              <button className="btn ghost" type="button" onClick={applyDelta}>Apply Delta</button>
            </div>
          </div>

          <div>
            <div className="row-head">
              <strong>Prompt Preview</strong>
              <button className="btn ghost mini" type="button" onClick={refreshPromptPreview}>刷新</button>
            </div>
            <pre className="code">{promptPreview || '(empty)'}</pre>
          </div>
        </div>
      </div>

      {/* 历史与审计 */}
      <div className="grid two-col">
        <div className="section nested">
          <h3>History（最近评估与事件）</h3>
          <div className="timeline-list">
            {evaluations.slice(0, 8).map(item => <HistoryRow item={item} kind="evaluation" key={`eval-${stringField(item, 'id')}`} />)}
            {events.slice(0, 8).map(item => <HistoryRow item={item} kind="event" key={`event-${stringField(item, 'id')}`} />)}
          </div>
        </div>
        <div className="section nested">
          <h3>Plugin Write Audit</h3>
          <div className="timeline-list">
            {pluginWrites.slice(0, 10).map(item => (
              <div className="timeline-item" key={stringField(item, 'id')}>
                <b>{stringField(item, 'plugin_id') || '-'}</b>
                <span>{stringField(item, 'capability')} / {stringField(item, 'request_kind')} / accepted={String(boolField(item, 'accepted'))}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* 原始数据（只读快照） */}
      <div className="grid two-col">
        <div className="section nested">
          <div className="row-head">
            <h3>Config Draft</h3>
            <span className="code-header-label">实时草稿</span>
          </div>
          <pre className="code">{configJSON}</pre>
        </div>
        <div className="section nested">
          <div className="row-head">
            <h3>Last Result</h3>
            <span className="code-header-label">最近结果</span>
          </div>
          <pre className="code">{Object.keys(lastResult).length ? resultJSON : pretty({})}</pre>
        </div>
      </div>
    </div>
  );
});

function HistoryRow({ item, kind }: { item: AnyRecord; kind: string }) {
  return (
    <div className="timeline-item">
      <b>{kind}: {stringField(item, 'status') || stringField(item, 'committed_by') || '-'}</b>
      <span>{formatTime(field(item, 'created_at', ''))}</span>
      <span>{stringField(item, 'cause_summary') || stringField(field(item, 'trigger', {}), 'trigger_type')}</span>
    </div>
  );
}

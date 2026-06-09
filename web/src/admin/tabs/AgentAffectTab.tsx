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
  configJSON,
  resultJSON,
  providers,
  modelOptions,
}: AgentAffectTabProps) {
  const mood = field<AnyRecord>(currentMood, 'mood', {});
  const moodVector = field<AnyRecord>(mood, 'vector', {});
  const evaluator = field<AnyRecord>(configDraft, 'evaluator', {});
  const evaluations = arrayField<AnyRecord>(history, 'evaluations');
  const events = arrayField<AnyRecord>(history, 'events');

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
          <span className="param-note">Persona / Session 决定下方所有实时数据</span>
        </div>
        <div className="grid compact">
          <div className="field">
            <label>Persona ID</label>
            <input value={personaID} onChange={event => setPersonaID(event.target.value)} />
          </div>
          <div className="field">
            <label>Session ID</label>
            <input value={sessionID} onChange={event => setSessionID(event.target.value)} />
          </div>
        </div>
      </div>

      {/* 全局开关与模式 */}
      <div className="grid">
        <label className="check"><input type="checkbox" checked={boolField(configDraft, 'enabled')} onChange={event => updateConfigPath(['enabled'], event.target.checked)} /> 启用 Agent Affect</label>
        <label className="check"><input type="checkbox" checked={boolField(configDraft, 'storage_enabled')} onChange={event => updateConfigPath(['storage_enabled'], event.target.checked)} /> SQLite 持久化</label>
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
        <label className="check"><input type="checkbox" checked={boolField(field(configDraft, 'prompt', {}), 'include_mood_block')} onChange={event => updateConfigPath(['prompt', 'include_mood_block'], event.target.checked)} /> 注入 prompt mood block</label>
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
            <span className="badge active">{stringField(mood, 'label') || '—'}</span>
          </div>
          <div className="kv">
            <span>Enabled</span><b>{String(boolField(currentMood, 'enabled'))}</b>
            <span>Confidence</span><b>{numberField(mood, 'confidence').toFixed(3)}</b>
            <span>Updated</span><b>{formatTime(field(mood, 'updated_at', ''))}</b>
          </div>
          <div className="pill-row">
            {vectorKeys.map(key => <span className="pill" key={key}>{key}: {numberField(moodVector, key).toFixed(3)}</span>)}
          </div>
          <pre className="code">{stringField(mood, 'cause_summary') || 'No cause summary.'}</pre>
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

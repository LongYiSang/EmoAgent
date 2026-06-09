import { memo } from 'react';
import { arrayField, boolField, field, formatTime, numberField, pretty, stringField } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import type { AgentAffectAdmin } from '../hooks/useAgentAffectAdmin';

const vectorKeys = ['valence', 'arousal', 'dominance', 'energy', 'warmth', 'concern', 'curiosity', 'playfulness', 'attachment', 'frustration', 'uncertainty'];

type AgentAffectTabProps = AgentAffectAdmin;

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
}: AgentAffectTabProps) {
  const mood = field<AnyRecord>(currentMood, 'mood', {});
  const moodVector = field<AnyRecord>(mood, 'vector', {});
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

      <div className="grid">
        <label>Persona ID<input value={personaID} onChange={event => setPersonaID(event.target.value)} /></label>
        <label>Session ID<input value={sessionID} onChange={event => setSessionID(event.target.value)} /></label>
        <label className="check"><input type="checkbox" checked={boolField(configDraft, 'enabled')} onChange={event => updateConfigPath(['enabled'], event.target.checked)} /> 启用 Agent Affect</label>
        <label className="check"><input type="checkbox" checked={boolField(configDraft, 'storage_enabled')} onChange={event => updateConfigPath(['storage_enabled'], event.target.checked)} /> SQLite 持久化</label>
        <label>Evaluator Mode<select value={stringField(field(configDraft, 'evaluator', {}), 'mode') || 'llm'} onChange={event => updateConfigPath(['evaluator', 'mode'], event.target.value)}><option value="llm">llm</option><option value="disabled">disabled</option></select></label>
        <label>Context Mode<select value={stringField(field(configDraft, 'context', {}), 'mode') || 'raw_window'} onChange={event => updateConfigPath(['context', 'mode'], event.target.value)}><option value="none">none</option><option value="raw_window">raw_window</option><option value="summary_window">summary_window</option><option value="mixed">mixed</option></select></label>
        <label className="check"><input type="checkbox" checked={boolField(field(configDraft, 'context', {}), 'store_raw_inputs')} onChange={event => updateConfigPath(['context', 'store_raw_inputs'], event.target.checked)} /> 保存 raw input_text</label>
        <label className="check"><input type="checkbox" checked={boolField(field(configDraft, 'prompt', {}), 'include_mood_block')} onChange={event => updateConfigPath(['prompt', 'include_mood_block'], event.target.checked)} /> 注入 prompt mood block</label>
      </div>

      <div className="grid two-col">
        <section>
          <h3>当前 Mood</h3>
          <div className="kv">
            <span>Enabled</span><b>{String(boolField(currentMood, 'enabled'))}</b>
            <span>Label</span><b>{stringField(mood, 'label') || '-'}</b>
            <span>Confidence</span><b>{numberField(mood, 'confidence').toFixed(3)}</b>
            <span>Updated</span><b>{formatTime(field(mood, 'updated_at', ''))}</b>
          </div>
          <div className="pill-row">
            {vectorKeys.map(key => <span className="pill" key={key}>{key}: {numberField(moodVector, key).toFixed(3)}</span>)}
          </div>
          <pre className="code">{stringField(mood, 'cause_summary') || 'No cause summary.'}</pre>
        </section>

        <section>
          <h3>Profile Baseline</h3>
          <div className="grid compact">
            {vectorKeys.map(key => (
              <label key={key}>{key}<input type="number" step="0.01" value={numberField(field(profileDraft, 'baseline', {}), key)} onChange={event => updateProfileBaseline(key, Number(event.target.value))} /></label>
            ))}
          </div>
          <div className="actions"><button className="btn ghost" type="button" onClick={saveProfileDraft}>保存 Profile</button></div>
        </section>
      </div>

      <div className="grid two-col">
        <section>
          <h3>手动评估</h3>
          <textarea rows={4} value={debugInput} onChange={event => setDebugInput(event.target.value)} />
          <div className="actions">
            <button className="btn ghost" type="button" onClick={evaluatePreview}>Evaluate</button>
            <button className="btn primary" type="button" onClick={submitCommit}>Submit</button>
            <button className="btn danger" type="button" onClick={resetMood}>Reset</button>
          </div>
          <textarea rows={6} value={deltaJSON} onChange={event => setDeltaJSON(event.target.value)} />
          <div className="actions"><button className="btn ghost" type="button" onClick={applyDelta}>Apply Delta</button></div>
        </section>

        <section>
          <div className="row-head">
            <h3>Prompt Preview</h3>
            <button className="btn ghost" type="button" onClick={refreshPromptPreview}>刷新</button>
          </div>
          <pre className="code">{promptPreview || '(empty)'}</pre>
        </section>
      </div>

      <div className="grid two-col">
        <section>
          <h3>History</h3>
          <div className="timeline-list">
            {evaluations.slice(0, 8).map(item => <HistoryRow item={item} kind="evaluation" key={`eval-${stringField(item, 'id')}`} />)}
            {events.slice(0, 8).map(item => <HistoryRow item={item} kind="event" key={`event-${stringField(item, 'id')}`} />)}
          </div>
        </section>
        <section>
          <h3>Plugin Write Audit</h3>
          <div className="timeline-list">
            {pluginWrites.slice(0, 10).map(item => (
              <div className="timeline-item" key={stringField(item, 'id')}>
                <b>{stringField(item, 'plugin_id') || '-'}</b>
                <span>{stringField(item, 'capability')} / {stringField(item, 'request_kind')} / accepted={String(boolField(item, 'accepted'))}</span>
              </div>
            ))}
          </div>
        </section>
      </div>

      <div className="grid two-col">
        <section><h3>Config JSON</h3><pre className="code">{configJSON}</pre></section>
        <section><h3>Last Result</h3><pre className="code">{Object.keys(lastResult).length ? resultJSON : pretty({})}</pre></section>
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

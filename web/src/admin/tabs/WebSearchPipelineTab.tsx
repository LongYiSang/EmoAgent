import { memo } from 'react';
import type { AnyRecord } from '../../shared/lib/api';
import { arrayField, boolField, field, stringField, toInt } from '../../shared/lib/data';
import { classNames } from '../../shared/lib/classNames';
import type { WebSearchAdmin } from '../hooks/useWebSearchAdmin';
import { Field } from '../components/Field';

export type WebSearchPipelineTabProps = WebSearchAdmin;

const rerankModelPresets = ['BAAI/bge-reranker-v2-m3', 'Pro/BAAI/bge-reranker-v2-m3'];

export default memo(function WebSearchPipelineTab({
  webSearchDraft,
  webSearchRuntime,
  webSearchIssues,
  webSearchJSON,
  reloadWebSearchConfig,
  updateWebSearchPath,
  rollbackToLegacyTavily,
  saveWebSearchDraft,
}: WebSearchPipelineTabProps) {
  const pipeline = field<AnyRecord>(webSearchDraft, 'pipeline', {});
  const search = field<AnyRecord>(pipeline, 'search', {});
  const reader = field<AnyRecord>(pipeline, 'reader', {});
  const rerank = field<AnyRecord>(pipeline, 'rerank', {});
  const provider = stringField(webSearchDraft, 'provider') || 'tavily';
  const pipelineActive = boolField(webSearchRuntime, 'pipeline_active');
  const registered = boolField(webSearchRuntime, 'registered');
  const effectiveMode = stringField(webSearchRuntime, 'effective_mode') || (boolField(webSearchDraft, 'enabled') ? 'unavailable' : 'disabled');
  const tavilyEnv = field<AnyRecord>(webSearchRuntime, 'tavily_env', {});
  const rerankEnv = field<AnyRecord>(webSearchRuntime, 'rerank_env', {});
  const runtimeWarnings = arrayField<unknown>(webSearchRuntime, 'warnings');
  const pipelineBadge = pipelineActive ? '生效' : '未生效';

  return (
    <div className="section">
      <div className="hero sticky-hero">
        <div>
          <div className="meta">web_search 的 Search / Reader / Rerank 运行时配置</div>
        </div>
        <div className="actions">
          <button className="btn ghost" type="button" onClick={reloadWebSearchConfig}>重新加载</button>
          <button className="btn primary" type="button" onClick={saveWebSearchDraft}>保存配置</button>
        </div>
      </div>

      <div className="grid">
        <label className="check"><input type="checkbox" checked={boolField(webSearchDraft, 'enabled')} onChange={event => updateWebSearchPath(['enabled'], event.target.checked)} /> 启用 web_search</label>
        <div className="field">
          <label htmlFor="websearch-provider">运行模式</label>
          <select id="websearch-provider" value={provider} onChange={event => updateWebSearchPath(['provider'], event.target.value)}>
            <option value="tavily">纯简单搜索</option>
            <option value="pipeline">流水线搜索</option>
          </select>
        </div>
        <label className="check"><input type="checkbox" checked={boolField(pipeline, 'enabled')} onChange={event => updateWebSearchPath(['pipeline', 'enabled'], event.target.checked)} /> 流水线配置启用</label>
        <NumberField id="websearch-max-results" label="Max Results" value={field(webSearchDraft, 'max_results', '')} min={1} onChange={value => updateWebSearchPath(['max_results'], value)} />
        <NumberField id="websearch-timeout-sec" label="Timeout Sec" value={field(webSearchDraft, 'timeout_sec', '')} min={0} onChange={value => updateWebSearchPath(['timeout_sec'], value)} />
        <label className="check"><input type="checkbox" checked={boolField(webSearchDraft, 'include_answer')} onChange={event => updateWebSearchPath(['include_answer'], event.target.checked)} /> Include Answer</label>
      </div>
      <div className="grid compact">
        <div className="field"><label>当前生效</label><span className={classNames('badge', registered ? 'ok' : 'warn')}>{effectiveModeLabel(effectiveMode)}</span></div>
        <div className="field"><label>运行模式</label><span className="badge">{runningModeLabel(provider)}</span></div>
        <div className="field"><label>流水线</label><span className={classNames('badge', pipelineActive ? 'ok' : 'warn')}>{pipelineBadge}</span></div>
        <div className="field"><label>Tavily Env</label><span className={classNames('badge', boolField(tavilyEnv, 'present') ? 'ok' : 'warn')}>{envLabel(tavilyEnv)}</span></div>
        <div className="field"><label>SiliconFlow Env</label><span className={classNames('badge', boolField(rerankEnv, 'present') ? 'ok' : 'warn')}>{envLabel(rerankEnv)}</span></div>
        <div className="field"><label>Warnings</label><span className={classNames('badge', runtimeWarnings.length > 0 && 'warn')}>{runtimeWarnings.length}</span></div>
      </div>
      <div className="actions foot">
        <button className="btn ghost" type="button" onClick={rollbackToLegacyTavily}>回滚到纯简单搜索</button>
      </div>

      <details className="slot" open>
        <summary className="slot-head">
          <strong>Search</strong>
          <span className={classNames('badge', !pipelineActive && 'warn')}>{pipelineActive ? `${stringField(search, 'default_profile') || 'auto'} / ${stringField(search, 'default_depth') || 'advanced'}` : pipelineBadge}</span>
        </summary>
        <div className="grid compact">
          <div className="field">
            <label htmlFor="websearch-profile">Profile</label>
            <select id="websearch-profile" value={stringField(search, 'default_profile') || 'auto'} onChange={event => updateWebSearchPath(['pipeline', 'search', 'default_profile'], event.target.value)}>
              <option value="auto">auto</option>
              <option value="fast">fast</option>
              <option value="news">news</option>
              <option value="official_docs">official_docs</option>
            </select>
          </div>
          <DepthSelect id="websearch-depth" label="Depth" value={stringField(search, 'default_depth') || 'advanced'} onChange={value => updateWebSearchPath(['pipeline', 'search', 'default_depth'], value)} />
          <DepthSelect id="websearch-fast-depth" label="Fast Depth" value={stringField(search, 'fast_depth') || 'basic'} onChange={value => updateWebSearchPath(['pipeline', 'search', 'fast_depth'], value)} />
          <NumberField id="websearch-max-subqueries" label="Max Subqueries" value={field(search, 'max_subqueries', '')} min={1} onChange={value => updateWebSearchPath(['pipeline', 'search', 'max_subqueries'], value)} />
          <NumberField id="websearch-candidate-cap" label="Candidate Cap" value={field(search, 'candidate_cap', '')} min={1} onChange={value => updateWebSearchPath(['pipeline', 'search', 'candidate_cap'], value)} />
          <NumberField id="websearch-per-query" label="Per-query Results" value={field(search, 'per_query_max_results', '')} min={1} onChange={value => updateWebSearchPath(['pipeline', 'search', 'per_query_max_results'], value)} />
        </div>
      </details>

      <details className="slot" open>
        <summary className="slot-head">
          <strong>Reader</strong>
          <span className={classNames('badge', !pipelineActive && 'warn')}>{pipelineActive ? `${boolField(reader, 'enabled') ? 'enabled' : 'disabled'} / top ${String(field(reader, 'top_n', 0))}` : pipelineBadge}</span>
        </summary>
        <div className="grid compact">
          <label className="check"><input type="checkbox" checked={boolField(reader, 'enabled')} onChange={event => updateWebSearchPath(['pipeline', 'reader', 'enabled'], event.target.checked)} /> 启用 Reader</label>
          <NumberField id="websearch-reader-top-n" label="Top URLs" value={field(reader, 'top_n', '')} min={0} onChange={value => updateWebSearchPath(['pipeline', 'reader', 'top_n'], value)} />
          <DepthSelect id="websearch-reader-depth" label="Extract Depth" value={stringField(reader, 'extract_depth') || 'basic'} onChange={value => updateWebSearchPath(['pipeline', 'reader', 'extract_depth'], value)} />
          <div className="field">
            <label htmlFor="websearch-reader-format">Format</label>
            <select id="websearch-reader-format" value={stringField(reader, 'format') || 'markdown'} onChange={event => updateWebSearchPath(['pipeline', 'reader', 'format'], event.target.value)}>
              <option value="markdown">markdown</option>
              <option value="text">text</option>
            </select>
          </div>
          <NumberField id="websearch-reader-timeout" label="Timeout Sec" value={field(reader, 'timeout_sec', '')} min={0} onChange={value => updateWebSearchPath(['pipeline', 'reader', 'timeout_sec'], value)} />
          <NumberField id="websearch-reader-doc-chars" label="Doc Chars" value={field(reader, 'max_chars_per_doc', '')} min={0} onChange={value => updateWebSearchPath(['pipeline', 'reader', 'max_chars_per_doc'], value)} />
          <NumberField id="websearch-reader-chunk-chars" label="Chunk Chars" value={field(reader, 'max_chunk_chars', '')} min={0} onChange={value => updateWebSearchPath(['pipeline', 'reader', 'max_chunk_chars'], value)} />
        </div>
      </details>

      <details className="slot" open>
        <summary className="slot-head">
          <strong>Rerank</strong>
          <span className={classNames('badge', !pipelineActive && 'warn')}>{pipelineActive ? `${stringField(rerank, 'provider') || 'siliconflow'} / ${stringField(rerank, 'model') || '未指定模型'}` : pipelineBadge}</span>
        </summary>
        <div className="grid compact">
          <label className="check"><input type="checkbox" checked={boolField(rerank, 'enabled')} onChange={event => updateWebSearchPath(['pipeline', 'rerank', 'enabled'], event.target.checked)} /> 启用 Rerank</label>
          <div className="field">
            <label htmlFor="websearch-rerank-provider">Rerank Provider</label>
            <select id="websearch-rerank-provider" value={stringField(rerank, 'provider') || 'siliconflow'} onChange={event => updateWebSearchPath(['pipeline', 'rerank', 'provider'], event.target.value)}>
              <option value="siliconflow">siliconflow</option>
              <option value="heuristic">heuristic</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
          <Field id="websearch-rerank-model" label="Model" value={stringField(rerank, 'model')} onChange={value => updateWebSearchPath(['pipeline', 'rerank', 'model'], value)} list="websearch-rerank-model-options" mono />
          <datalist id="websearch-rerank-model-options">
            {rerankModelPresets.map(model => <option key={model} value={model} />)}
          </datalist>
          <div className="field">
            <label htmlFor="websearch-rerank-fallback">Fallback</label>
            <select id="websearch-rerank-fallback" value={stringField(rerank, 'fallback') || 'heuristic'} onChange={event => updateWebSearchPath(['pipeline', 'rerank', 'fallback'], event.target.value)}>
              <option value="heuristic">heuristic</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
          <NumberField id="websearch-rerank-input-top-n" label="Input TopN" value={field(rerank, 'input_top_n', '')} min={0} onChange={value => updateWebSearchPath(['pipeline', 'rerank', 'input_top_n'], value)} />
          <NumberField id="websearch-rerank-top-k" label="TopK" value={field(rerank, 'top_k', '')} min={0} onChange={value => updateWebSearchPath(['pipeline', 'rerank', 'top_k'], value)} />
          <NumberField id="websearch-rerank-timeout" label="Timeout Sec" value={field(rerank, 'timeout_sec', '')} min={0} onChange={value => updateWebSearchPath(['pipeline', 'rerank', 'timeout_sec'], value)} />
          <NumberField id="websearch-rerank-doc-chars" label="Max Doc Chars" value={field(rerank, 'max_doc_chars', '')} min={0} onChange={value => updateWebSearchPath(['pipeline', 'rerank', 'max_doc_chars'], value)} />
        </div>
      </details>

      <details className="slot">
        <summary className="slot-head"><strong>Advanced</strong><span className="badge">{webSearchIssues.length ? `${webSearchIssues.length} issues` : 'clean'}</span></summary>
        <div className="grid compact">
          <Field id="websearch-base-url" label="Tavily Base URL" value={stringField(webSearchDraft, 'base_url')} onChange={value => updateWebSearchPath(['base_url'], value)} mono />
          <Field id="websearch-api-key-env" label="Tavily Key Env" value={stringField(webSearchDraft, 'api_key_env')} onChange={value => updateWebSearchPath(['api_key_env'], value)} mono />
          <Field id="websearch-rerank-base-url" label="SiliconFlow Base URL" value={stringField(rerank, 'base_url')} onChange={value => updateWebSearchPath(['pipeline', 'rerank', 'base_url'], value)} mono />
          <Field id="websearch-rerank-path" label="SiliconFlow Path" value={stringField(rerank, 'path')} onChange={value => updateWebSearchPath(['pipeline', 'rerank', 'path'], value)} mono />
          <Field id="websearch-rerank-api-key-env" label="SiliconFlow Key Env" value={stringField(rerank, 'api_key_env')} onChange={value => updateWebSearchPath(['pipeline', 'rerank', 'api_key_env'], value)} mono />
        </div>
        <div className="section nested">
          <div className="row-head"><h3>当前 Issues</h3><span className="badge">{webSearchIssues.length}</span></div>
          <div className="timeline-list">
            {webSearchIssues.length ? webSearchIssues.map((issue, index) => (
              <div className="timeline-item" key={`${String(field(issue, 'path', 'issue'))}-${index}`}>
                <b>{String(field(issue, 'path', 'websearch'))}<span className={classNames('badge', String(field(issue, 'severity', '')) === 'error' && 'warn')}>{String(field(issue, 'severity', 'info'))}</span></b>
                <span>{String(field(issue, 'message', ''))}</span>
              </div>
            )) : <div className="hint">暂无 websearch 配置问题</div>}
          </div>
        </div>
        <div className="section nested">
          <div className="row-head"><h3>Runtime Warnings</h3><span className="badge">{runtimeWarnings.length}</span></div>
          <div className="timeline-list">
            {runtimeWarnings.length ? runtimeWarnings.map((warning, index) => (
              <div className="timeline-item" key={`${String(warning)}-${index}`}>
                <b>websearch_runtime<span className="badge warn">warning</span></b>
                <span>{String(warning)}</span>
              </div>
            )) : <div className="hint">暂无运行时警告</div>}
          </div>
        </div>
        <pre className="code" id="websearch-config-json">{webSearchJSON}</pre>
      </details>
    </div>
  );
});

function DepthSelect({ id, label, value, onChange }: { id: string; label: string; value: string; onChange: (value: string) => void }) {
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <select id={id} value={value} onChange={event => onChange(event.target.value)}>
        <option value="basic">basic</option>
        <option value="advanced">advanced</option>
      </select>
    </div>
  );
}

function runningModeLabel(value: string) {
  return value === 'pipeline' ? '流水线搜索' : '纯简单搜索';
}

function effectiveModeLabel(value: string) {
  switch (value) {
    case 'pipeline':
      return '流水线搜索';
    case 'tavily':
      return '纯简单搜索';
    case 'disabled':
      return '已禁用';
    default:
      return '不可用';
  }
}

function envLabel(env: AnyRecord) {
  const name = stringField(env, 'api_key_env') || '未配置';
  return `${name}: ${boolField(env, 'present') ? 'present' : 'missing'}`;
}

function NumberField({ id, label, value, min, onChange }: { id: string; label: string; value: unknown; min: number; onChange: (value: number | undefined) => void }) {
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input id={id} type="number" min={min} value={String(value ?? '')} onChange={event => onChange(toInt(event.target.value))} />
    </div>
  );
}

import { memo } from 'react';
import { boolField, field, pretty, toInt } from '../../shared/lib/data';
import type { AnyRecord } from '../../shared/lib/api';
import { Field } from '../components/Field';

export type RetrievalTabProps = {
  memoryDraft: AnyRecord;
  effectiveConfig: AnyRecord;
  updateMemoryPath: (path: string[], value: unknown) => void;
  saveRetrieval: () => Promise<void>;
};

export default memo(function RetrievalTab({ memoryDraft, effectiveConfig, updateMemoryPath, saveRetrieval }: RetrievalTabProps) {
  const retrieval = field<AnyRecord>(memoryDraft, 'retrieval', {});

  function setRetrieval(key: string, value: unknown) {
    updateMemoryPath(['retrieval', key], value);
  }

  return (
    <div className="section">
      <div className="hero"><div><h2>Retrieval/Mirror</h2><div className="meta">SQLite remains authority; mirror is degradable</div></div><button className="btn primary" id="save-retrieval" type="button" onClick={saveRetrieval}>Save</button></div>
      <div className="grid">
        <label className="check"><input id="retrieval-enabled" type="checkbox" checked={boolField(retrieval, 'enabled')} onChange={event => setRetrieval('enabled', event.target.checked)} /> Retrieval</label>
        <label className="check"><input id="retrieval-inject" type="checkbox" checked={boolField(retrieval, 'inject_prompt')} onChange={event => setRetrieval('inject_prompt', event.target.checked)} /> Inject prompt</label>
        <label className="check"><input id="retrieval-fts" type="checkbox" checked={boolField(retrieval, 'use_fts')} onChange={event => setRetrieval('use_fts', event.target.checked)} /> Use FTS</label>
        <label className="check"><input id="retrieval-mirror" type="checkbox" checked={boolField(retrieval, 'use_mirror')} onChange={event => setRetrieval('use_mirror', event.target.checked)} /> Use mirror</label>
        <Field id="retrieval-final-count" type="number" label="Final count" value={String(retrieval.final_memory_count || '')} onChange={value => setRetrieval('final_memory_count', toInt(value))} />
        <Field id="retrieval-budget" type="number" label="Context budget" value={String(retrieval.context_budget_tokens || '')} onChange={value => setRetrieval('context_budget_tokens', toInt(value))} />
      </div>
      <pre className="code" id="retrieval-mirror-json">{pretty({ retrieval: field(field(effectiveConfig, 'memory_core', {}), 'retrieval', {}), mirror: field(field(effectiveConfig, 'memory_core', {}), 'mirror', {}) })}</pre>
    </div>
  );
});

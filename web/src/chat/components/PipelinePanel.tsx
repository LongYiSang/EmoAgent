import { useMemo } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { field, pretty, stringField } from '../../shared/lib/data';

export function PipelinePanel({ snapshot, onClose }: { snapshot: unknown; onClose: () => void }) {
  const stages = useMemo(() => {
    const raw = field<Record<string, unknown>>(snapshot, 'stages', {});
    return ['anchor_recall', 'rrf_fusion', 'sqlite_authority_filter', 'safe_rerank', 'final_selection_mmr'].map(key => ({ key, items: Array.isArray(raw[key]) ? raw[key] as unknown[] : [] }));
  }, [snapshot]);
  return (
    <aside id="memory-pipeline-panel" className={classNames('memory-pipeline-panel', Boolean(snapshot) && 'open')} aria-hidden={!snapshot}>
      <div className="memory-pipeline-top"><strong>记忆管线</strong><button className="btn ghost" id="memory-pipeline-close" type="button" onClick={onClose}>关闭</button></div>
      <div id="memory-pipeline-panel-body" className="memory-pipeline-panel-body">
        <section className="memory-pipeline-section"><div className="memory-pipeline-section-title">prompt_block</div><pre className="memory-pipeline-prompt">{stringField(snapshot, 'prompt_block') || stringField(snapshot, 'promptBlock') || '（空）'}</pre></section>
        <section className="memory-pipeline-section"><div className="memory-pipeline-section-title">query_analysis</div><pre className="memory-pipeline-json">{pretty(field(snapshot, 'query_analysis', field(snapshot, 'queryAnalysis', {})))}</pre></section>
        <section className="memory-pipeline-section"><div className="memory-pipeline-section-title">stages</div>{stages.map(stage => <div className="memory-pipeline-stage" key={stage.key}><div className="memory-pipeline-stage-name">{stage.key}</div>{stage.items.length ? stage.items.map((item, index) => <div className="memory-pipeline-item" key={index}><span>{stringField(item, 'content_summary') || stringField(item, 'contentSummary') || '（空）'}</span><span className="memory-pipeline-score">{String(field(item, 'score', ''))}</span></div>) : <div className="memory-pipeline-empty">无记录</div>}</div>)}</section>
      </div>
    </aside>
  );
}

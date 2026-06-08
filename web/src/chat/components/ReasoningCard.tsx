import { useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { numberField, stringField } from '../../shared/lib/data';
import type { ReasoningActivity } from '../protocol/wsTypes';
import { formatReasoningDuration } from '../lib/chatViewData';

export function ReasoningCard({ reasoning, collapsed, onOpenPipeline }: { reasoning: ReasoningActivity; collapsed: boolean; onOpenPipeline: (snapshot: unknown) => void }) {
  const [open, setOpen] = useState(!collapsed);
  const status = stringField(reasoning, 'status') || 'running';
  const duration = numberField(reasoning, 'duration_ms') || numberField(reasoning, 'durationMS');
  const pipeline = reasoning.memory_pipeline || reasoning.memoryPipeline;
  const title = status === 'running' ? '思考中...' : duration ? `思考了 ${formatReasoningDuration(duration)}` : '思考完成';
  return (
    <div className={classNames('reasoning', status, open && 'expanded')}>
      <div className="reasoning-av">思</div>
      <div className="reasoning-card">
        <div className="reasoning-head">
          <button className="reasoning-toggle" type="button" aria-expanded={open} onClick={() => setOpen(value => !value)}>
            <span className="reasoning-title">{title}</span>
            <span className="reasoning-badge">{status === 'running' ? 'thinking' : status || 'thinking'}</span>
            <span className="reasoning-caret">⌄</span>
          </button>
          {pipeline && <button className="memory-pipeline-btn" type="button" onClick={() => onOpenPipeline(pipeline)}>记忆管线</button>}
        </div>
        {open && <div className="reasoning-body"><pre className="reasoning-preview">{stringField(reasoning, 'content') || 'Waiting for reasoning...'}</pre><div className="reasoning-meta">{duration ? <span>{duration} ms</span> : null}{stringField(reasoning, 'provider') ? <span>{stringField(reasoning, 'provider')}</span> : null}{stringField(reasoning, 'model') ? <span>{stringField(reasoning, 'model')}</span> : null}</div></div>}
      </div>
    </div>
  );
}

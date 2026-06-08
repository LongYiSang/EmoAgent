import { useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { numberField, stringField } from '../../shared/lib/data';
import type { ReasoningActivity } from '../protocol/wsTypes';
import { formatReasoningDuration } from '../lib/chatViewData';

function reasoningStatusLabel(status: string) {
  if (status === 'running') return '思考中';
  if (status === 'done' || status === 'success' || status === 'completed') return '完成';
  if (status === 'error' || status === 'failed') return '失败';
  return status || '思考';
}

export function ReasoningCard({ reasoning, collapsed, open: controlledOpen, onOpenChange, onOpenPipeline }: {
  reasoning: ReasoningActivity;
  collapsed: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  onOpenPipeline: (snapshot: unknown) => void;
}) {
  const [open, setOpen] = useState(!collapsed);
  const actualOpen = controlledOpen ?? open;
  const status = stringField(reasoning, 'status') || 'running';
  const duration = numberField(reasoning, 'duration_ms') || numberField(reasoning, 'durationMS');
  const pipeline = reasoning.memory_pipeline || reasoning.memoryPipeline;
  const title = status === 'running' ? '思考中...' : duration ? `思考了 ${formatReasoningDuration(duration)}` : '思考完成';
  const toggleOpen = () => {
    const nextOpen = !actualOpen;
    if (onOpenChange) onOpenChange(nextOpen);
    else setOpen(nextOpen);
  };
  return (
    <div className={classNames('reasoning', status, actualOpen && 'expanded')}>
      <div className="reasoning-av">思</div>
      <div className="reasoning-card">
        <div className="reasoning-head">
          <button className="reasoning-toggle" type="button" aria-expanded={actualOpen} onClick={toggleOpen}>
            <span className="reasoning-title">{title}</span>
          </button>
          <div className="reasoning-actions">
            {pipeline && <button className="memory-pipeline-btn" type="button" onClick={() => onOpenPipeline(pipeline)}>记忆管线</button>}
            <button className="reasoning-state-toggle" type="button" aria-expanded={actualOpen} aria-label={actualOpen ? '收起思考详情' : '展开思考详情'} onClick={toggleOpen}>
              <span className="reasoning-badge">{reasoningStatusLabel(status)}</span>
              <span className="reasoning-disclosure" aria-hidden="true" />
            </button>
          </div>
        </div>
        {actualOpen && <div className="reasoning-body"><pre className="reasoning-preview">{stringField(reasoning, 'content') || '正在等待思考内容...'}</pre><div className="reasoning-meta">{duration ? <span>{duration} ms</span> : null}{stringField(reasoning, 'provider') ? <span>{stringField(reasoning, 'provider')}</span> : null}{stringField(reasoning, 'model') ? <span>{stringField(reasoning, 'model')}</span> : null}</div></div>}
      </div>
    </div>
  );
}

import { useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { numberField, stringField } from '../../shared/lib/data';
import { Avatar } from '../../shared/components/Avatar';
import type { ToolActivity } from '../protocol/wsTypes';
import { toolStatusLabel } from '../lib/chatViewData';

export function ToolCard({ tool, collapsed, open: controlledOpen, onOpenChange }: {
  tool: ToolActivity;
  collapsed: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const [open, setOpen] = useState(!collapsed);
  const actualOpen = controlledOpen ?? open;
  const status = stringField(tool, 'status') || 'running';
  const duration = numberField(tool, 'duration_ms') || numberField(tool, 'durationMS');
  const toggleOpen = () => {
    const nextOpen = !actualOpen;
    if (onOpenChange) onOpenChange(nextOpen);
    else setOpen(nextOpen);
  };
  return (
    <div className={classNames('tool', status, actualOpen && 'expanded')}>
      <Avatar role="tool" />
      <div className="tool-card">
        <button className="tool-head" type="button" aria-expanded={actualOpen} onClick={toggleOpen}>
          <span className="tool-title">{stringField(tool, 'name') || '工具'}</span>
          <span className="tool-badge">{toolStatusLabel(status)}</span>
          <span className="tool-caret" aria-hidden="true" />
        </button>
        {actualOpen && <div className="tool-body"><pre className="tool-preview">{stringField(tool, 'preview') || '正在等待结果...'}</pre><div className="tool-meta">{duration ? <span>{duration} ms</span> : null}{numberField(tool, 'size') ? <span>{numberField(tool, 'size')} bytes</span> : null}{stringField(tool, 'hash') ? <span>hash {stringField(tool, 'hash')}</span> : null}</div></div>}
      </div>
    </div>
  );
}

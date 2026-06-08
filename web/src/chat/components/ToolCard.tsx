import { useState } from 'react';
import { classNames } from '../../shared/lib/classNames';
import { numberField, stringField } from '../../shared/lib/data';
import type { ToolActivity } from '../protocol/wsTypes';
import { toolStatusLabel } from '../lib/chatViewData';

export function ToolCard({ tool, collapsed }: { tool: ToolActivity; collapsed: boolean }) {
  const [open, setOpen] = useState(!collapsed);
  const status = stringField(tool, 'status') || 'running';
  const duration = numberField(tool, 'duration_ms') || numberField(tool, 'durationMS');
  return (
    <div className={classNames('tool', status, open && 'expanded')}>
      <div className="tool-av">T</div>
      <div className="tool-card">
        <button className="tool-head" type="button" aria-expanded={open} onClick={() => setOpen(value => !value)}>
          <span className="tool-title">{stringField(tool, 'name') || 'tool'}</span>
          <span className="tool-badge">{toolStatusLabel(status)}</span>
          <span className="tool-caret">⌄</span>
        </button>
        {open && <div className="tool-body"><pre className="tool-preview">{stringField(tool, 'preview') || 'Waiting for result...'}</pre><div className="tool-meta">{duration ? <span>{duration} ms</span> : null}{numberField(tool, 'size') ? <span>{numberField(tool, 'size')} bytes</span> : null}{stringField(tool, 'hash') ? <span>hash {stringField(tool, 'hash')}</span> : null}</div></div>}
      </div>
    </div>
  );
}

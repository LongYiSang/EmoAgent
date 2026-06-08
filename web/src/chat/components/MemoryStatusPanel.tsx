import { classNames } from '../../shared/lib/classNames';
import { stringField } from '../../shared/lib/data';
import type { MemoryJob, MemorySegment } from '../protocol/memoryApi';
import { memorySegmentLabel, memoryStatusOf } from '../lib/chatViewData';

export function MemoryStatusPanel({ visible, segments, jobs }: { visible: boolean; segments: MemorySegment[]; jobs: MemoryJob[] }) {
  if (!visible) return <div id="memory-status-panel" className="memory-status-panel" />;
  return (
    <div id="memory-status-panel" className="memory-status-panel visible">
      <MemoryBlock title="Segments" empty="暂无 segment" items={segments.slice(0, 4)} label={memorySegmentLabel} />
      <MemoryBlock title="Jobs" empty="暂无 job" items={jobs.slice(0, 4)} label={item => `${stringField(item, 'trigger') || 'extraction'} · ${String(stringField(item, 'id')).slice(0, 8)}`} />
    </div>
  );
}

function MemoryBlock({ title, empty, items, label }: { title: string; empty: string; items: unknown[]; label: (item: unknown) => string }) {
  return (
    <div className="memory-status-block">
      <div className="memory-status-title">{title}</div>
      {items.length ? items.map((item, index) => {
        const status = memoryStatusOf(item);
        return (
          <div className="memory-status-row" key={index}>
            <span>{label(item)}</span>
            <span className={classNames('memory-badge', status)}>{status}</span>
          </div>
        );
      }) : (
        <div className="memory-status-row"><span>{empty}</span><span className="memory-badge">empty</span></div>
      )}
    </div>
  );
}

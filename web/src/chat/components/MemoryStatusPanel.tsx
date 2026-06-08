import { classNames } from '../../shared/lib/classNames';
import { stringField } from '../../shared/lib/data';
import type { MemoryJob, MemorySegment } from '../protocol/memoryApi';
import { memorySegmentLabel, memoryStatusOf } from '../lib/chatViewData';

export function MemoryStatusPanel({ visible, segments, jobs }: { visible: boolean; segments: MemorySegment[]; jobs: MemoryJob[] }) {
  if (!visible) return <div id="memory-status-panel" className="memory-status-panel" />;
  return (
    <div id="memory-status-panel" className="memory-status-panel visible">
      <MemoryBlock title="片段" empty="暂无片段" items={segments.slice(0, 4)} label={memorySegmentLabel} />
      <MemoryBlock title="任务" empty="暂无任务" items={jobs.slice(0, 4)} label={item => `${memoryTriggerLabel(stringField(item, 'trigger'))} · ${String(stringField(item, 'id')).slice(0, 8)}`} />
    </div>
  );
}

function memoryTriggerLabel(trigger: string) {
  if (!trigger || trigger === 'extraction') return '提取';
  if (trigger === 'manual') return '手动';
  if (trigger === 'scheduled') return '定时';
  return trigger;
}

function memoryStatusLabel(status: string) {
  if (status === 'never') return '未提取';
  if (status === 'queued' || status === 'pending') return '排队中';
  if (status === 'running' || status === 'extracting') return '提取中';
  if (status === 'done' || status === 'success' || status === 'completed') return '完成';
  if (status === 'failed' || status === 'error') return '失败';
  if (status === 'skipped') return '已跳过';
  if (status === 'empty') return '空';
  return status;
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
            <span className={classNames('memory-badge', status)}>{memoryStatusLabel(status)}</span>
          </div>
        );
      }) : (
        <div className="memory-status-row"><span>{empty}</span><span className="memory-badge">空</span></div>
      )}
    </div>
  );
}

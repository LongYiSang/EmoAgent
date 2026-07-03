import { classNames } from '../../shared/lib/classNames';
import { Avatar } from '../../shared/components/Avatar';
import type { TimelineItem } from '../state/chatTypes';

type CommandItem = Extract<TimelineItem, { kind: 'command_result' }>;
type ContextSwitchItem = Extract<TimelineItem, { kind: 'context_switched' }>;

export function CommandResultEntry({ item }: { item: CommandItem | ContextSwitchItem }) {
  const failed = item.kind === 'command_result' && item.status === 'failed';
  const title = item.kind === 'context_switched'
    ? contextSwitchTitle(item.reason)
    : commandTitle(item.commandName, item.status);
  return (
    <div className={classNames('command-event', failed && 'failed')}>
      <Avatar role={failed ? 'error' : 'tool'} />
      <div className="command-card">
        <div className="command-title">
          <span>{title}</span>
          {item.kind === 'command_result' && item.commandName ? <code>/{item.commandName}</code> : null}
        </div>
        {item.content ? <div className="command-content">{item.content}</div> : null}
      </div>
    </div>
  );
}

function commandTitle(name: string, status: string) {
  if (status === 'failed') return '命令失败';
  if (!name) return '命令结果';
  return '命令结果';
}

function contextSwitchTitle(reason: string) {
  if (reason === 'new') return '已进入新会话';
  if (reason === 'switch') return '已切换会话';
  if (reason === 'reset') return '上下文已重置';
  return '会话已更新';
}

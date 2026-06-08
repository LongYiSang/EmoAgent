import { classNames } from '../../shared/lib/classNames';
import { Avatar } from '../../shared/components/Avatar';
import type { TimelineItem } from '../state/chatTypes';

export function MessageBubble({ item, onRetry }: { item: Extract<TimelineItem, { kind: 'message' }>; onRetry: () => void }) {
  const role = item.role === 'user' ? 'user' : item.role === 'error' ? 'error' : 'emotion';
  return (
    <div className={classNames('msg', item.role, item.status === 'pending' && 'pending', item.status === 'failed' && 'failed')}>
      <Avatar role={role} />
      <div className="bubble">
        <div className="message-content">{item.content}</div>
        {item.status === 'pending' && <div className="message-status">正在发送...</div>}
        {item.status === 'failed' && (
          <>
            <div className="message-status">发送失败</div>
            <button className="message-retry" type="button" onClick={onRetry}>重试</button>
          </>
        )}
      </div>
    </div>
  );
}

import { classNames } from '../../shared/lib/classNames';
import type { TimelineItem } from '../state/chatTypes';

export function MessageBubble({ item, onRetry }: { item: Extract<TimelineItem, { kind: 'message' }>; onRetry: () => void }) {
  return (
    <div className={classNames('msg', item.role, item.status === 'pending' && 'pending', item.status === 'failed' && 'failed')}>
      <div className="msg-av">{item.role === 'user' ? 'U' : item.role === 'error' ? '!' : 'E'}</div>
      <div className="bubble">
        <div className="message-content">{item.content}</div>
        {item.status === 'pending' && <div className="message-status">Sending...</div>}
        {item.status === 'failed' && (
          <>
            <div className="message-status">Send failed</div>
            <button className="message-retry" type="button" onClick={onRetry}>Retry</button>
          </>
        )}
      </div>
    </div>
  );
}

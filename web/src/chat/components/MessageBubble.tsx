import { classNames } from '../../shared/lib/classNames';
import { Avatar } from '../../shared/components/Avatar';
import type { TimelineItem } from '../state/chatTypes';

export function MessageBubble({ item, onRetry }: { item: Extract<TimelineItem, { kind: 'message' }>; onRetry: () => void }) {
  const role = item.role === 'user' ? 'user' : item.role === 'error' ? 'error' : 'emotion';
  const displayParts = item.displayParts?.length ? item.displayParts : undefined;
  return (
    <div className={classNames('msg', item.role, item.status === 'pending' && 'pending', item.status === 'failed' && 'failed')}>
      <Avatar role={role} />
      <div className="bubble">
        {displayParts ? (
          <div className="message-parts">
            {displayParts.map((part, index) => {
              if (part.type === 'text') {
                return <div className="message-part-text" key={`text-${index}`}>{part.text}</div>;
              }
              if (part.type === 'image') {
                if (!part.display_url) {
                  return <div className="message-part-text" key={`image-${index}`}>[used image]</div>;
                }
                return (
                  <a className="message-image-link" href={part.display_url} target="_blank" rel="noreferrer" key={`image-${part.media_asset_id || index}`}>
                    <img
                      className="message-image"
                      src={part.display_url}
                      alt="uploaded image"
                      loading="lazy"
                      width={part.width || undefined}
                      height={part.height || undefined}
                    />
                  </a>
                );
              }
              return null;
            })}
          </div>
        ) : (
          <div className="message-content">{item.content}</div>
        )}
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

import { classNames } from '../../shared/lib/classNames';
import { Avatar } from '../../shared/components/Avatar';
import type { TimelineItem } from '../state/chatTypes';
import { Markdown } from './Markdown';

export function MessageBubble({ item, streaming, onRetry }: {
  item: Extract<TimelineItem, { kind: 'message' }>;
  streaming?: boolean;
  onRetry: () => void;
}) {
  const role = item.role === 'user' ? 'user' : item.role === 'error' ? 'error' : 'emotion';
  const displayParts = item.displayParts?.length ? item.displayParts : undefined;
  const richText = role === 'emotion';
  // Grouping defaults to standalone so a message still renders correctly if it
  // reaches the bubble without having been annotated.
  const groupStart = item.groupStart !== false;
  const groupEnd = item.groupEnd !== false;
  return (
    <div className={classNames(
      'msg',
      item.role,
      groupStart && 'group-start',
      groupEnd && 'group-end',
      !groupStart && 'group-cont',
      item.status === 'pending' && 'pending',
      item.status === 'failed' && 'failed',
      streaming && 'streaming',
    )}>
      {/* One avatar per turn, not per delivered segment. */}
      {groupStart && <Avatar role={role} />}
      <div className="bubble">
        {displayParts ? (
          <div className="message-parts">
            {displayParts.map((part, index) => {
              if (part.type === 'text') {
                return richText
                  ? <Markdown className="message-part-text markdown-body" content={part.text || ''} key={`text-${index}`} />
                  : <div className="message-part-text" key={`text-${index}`}>{part.text}</div>;
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
        ) : richText ? (
          <Markdown className="message-content markdown-body" content={item.content} />
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
      {groupEnd && <time className="msg-time" dateTime={item.createdAt}>{formatClock(item.createdAt)}</time>}
    </div>
  );
}

function formatClock(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
}

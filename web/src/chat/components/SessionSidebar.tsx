import { classNames } from '../../shared/lib/classNames';
import { field, formatTime, stringField } from '../../shared/lib/data';
import type { SessionSummary } from '../protocol/sessionApi';
import { previewText, sessionIDOf, sessionPersonaOf } from '../lib/chatViewData';

export function SessionSidebar({
  open,
  sessions,
  currentPersonaKey,
  currentSessionId,
  onRefresh,
  onNew,
  onOpen,
  onDelete,
}: {
  open: boolean;
  sessions: SessionSummary[];
  currentPersonaKey: string;
  currentSessionId: string;
  onRefresh: () => void;
  onNew: () => void;
  onOpen: (sessionID: string, personaKey: string) => void;
  onDelete: (sessionID: string) => void;
}) {
  return (
    <aside className={classNames('chat-sidebar', open && 'open')} id="sidebar">
      <div className="side-head">
        <div>
          <div className="label">Persona</div>
          <strong id="sidebar-persona">{currentPersonaKey || '加载中...'}</strong>
        </div>
        <button className="btn ghost" id="refresh-sessions" type="button" onClick={onRefresh}>刷新</button>
      </div>
      <button className="btn primary w-full" id="new-chat" type="button" onClick={onNew}>新对话</button>
      <div className="session-list" id="session-list">
        {sessions.length ? sessions.map(item => (
          <SessionRow
            key={sessionIDOf(item)}
            item={item}
            active={sessionIDOf(item) === currentSessionId}
            onOpen={() => onOpen(sessionIDOf(item), sessionPersonaOf(item) || currentPersonaKey)}
            onDelete={() => onDelete(sessionIDOf(item))}
          />
        )) : <div className="session-empty">当前 Persona 还没有非空会话。</div>}
      </div>
    </aside>
  );
}

function SessionRow({ item, active, onOpen, onDelete }: { item: SessionSummary; active: boolean; onOpen: () => void; onDelete: () => void }) {
  const id = sessionIDOf(item);
  const title = stringField(item, 'title');
  const time = formatTime(field(item, 'updated_at', field(item, 'UpdatedAt', '')));
  const count = field<number>(item, 'message_count', field<number>(item, 'MessageCount', 0));

  const handleDelete = (event: React.MouseEvent | React.KeyboardEvent) => {
    event.stopPropagation();
    const displayName = title || time || '无标题会话';
    if (!window.confirm(`确认删除会话「${displayName}」？\n删除后无法恢复。`)) return;
    onDelete();
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onOpen();
    }
  };

  return (
    <div className={classNames('session', active && 'active')}>
      <div
        className="session-body"
        role="button"
        tabIndex={0}
        onClick={onOpen}
        onKeyDown={handleKeyDown}
        aria-label={`打开会话 ${title || time}`}
      >
        <span className="s-title">{title || time}</span>
        <span className="s-meta">{time} · {count} 条消息</span>
        <span className="s-preview">{previewText(item)}</span>
        <span className="sr-only">{id}</span>
      </div>

      <button
        className="s-del"
        type="button"
        aria-label={`删除会话 ${title || time || id}`}
        title="删除会话"
        onClick={handleDelete}
      >
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.25"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <polyline points="3 6 5 6 21 6" />
          <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
          <line x1="10" y1="11" x2="10" y2="17" />
          <line x1="14" y1="11" x2="14" y2="17" />
        </svg>
      </button>
    </div>
  );
}

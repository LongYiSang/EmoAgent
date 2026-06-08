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
          <strong id="sidebar-persona">{currentPersonaKey || 'loading...'}</strong>
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
  return (
    <button className={classNames('session', active && 'active')} type="button" onClick={onOpen}>
      <span className="s-title">{title || time}</span>
      <span className="s-meta">{time} · {count} messages</span>
      <span className="s-preview">{previewText(item)}</span>
      <span className="s-del" role="button" tabIndex={0} onClick={event => { event.stopPropagation(); onDelete(); }}>删除</span>
      <span className="sr-only">{id}</span>
    </button>
  );
}

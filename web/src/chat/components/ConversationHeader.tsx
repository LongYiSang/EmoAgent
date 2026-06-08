import { classNames } from '../../shared/lib/classNames';

export function ConversationHeader({
  subtitle,
  status,
  memoryStatusVisible,
  hasSession,
  onToggleSidebar,
  onToggleMemory,
  onScanMemory,
}: {
  subtitle: string;
  status: string;
  memoryStatusVisible: boolean;
  hasSession: boolean;
  onToggleSidebar: () => void;
  onToggleMemory: () => void;
  onScanMemory: () => void;
}) {
  return (
    <header className="conv-top">
      <button className="btn ghost mobile-toggle" id="toggle-sidebar" type="button" onClick={onToggleSidebar}>会话</button>
      <div className="conv-title-block">
        <h1 id="conv-title">EmoAgent</h1>
        <p id="subtitle">{subtitle}</p>
      </div>
      <div className="conv-actions">
        <span className="status-chip" id="status"><span className="dot" />{status}</span>
        <button
          className={classNames('btn ghost', memoryStatusVisible && 'active')}
          id="memory-status-toggle"
          type="button"
          disabled={!hasSession}
          aria-expanded={memoryStatusVisible}
          onClick={onToggleMemory}
        >
          记忆状态
        </button>
        <button className="btn primary" id="memory-scan" type="button" disabled={!hasSession} onClick={onScanMemory}>记忆扫描</button>
      </div>
    </header>
  );
}

import { classNames } from '../lib/classNames';

export function AppRail({ active, avatar }: { active: 'chat' | 'admin'; avatar: string }) {
  return (
    <nav className="rail" aria-label="Primary">
      <a className="rail-logo" href="/" aria-label="EmoAgent">E</a>
      <a className={classNames('rail-btn', active === 'chat' && 'active')} href="/" aria-label="Chat">聊<span className="rail-tooltip">Chat</span></a>
      <a className={classNames('rail-btn', active === 'admin' && 'active')} href="/admin.html" aria-label="Admin">设<span className="rail-tooltip">Admin</span></a>
      <div className="rail-spacer" />
      <div className="rail-avatar">{avatar}</div>
    </nav>
  );
}

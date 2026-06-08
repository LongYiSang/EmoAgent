import { classNames } from '../lib/classNames';
import { Avatar } from './Avatar';

export function AppRail({ active }: { active: 'chat' | 'admin' }) {
  return (
    <nav className="rail" aria-label="主导航">
      <a className="rail-logo" href="/" aria-label="EmoAgent">E</a>
      <a className={classNames('rail-btn', active === 'chat' && 'active')} href="/" aria-label="聊天">聊<span className="rail-tooltip">聊天</span></a>
      <a className={classNames('rail-btn', active === 'admin' && 'active')} href="/admin.html" aria-label="配置">设<span className="rail-tooltip">配置</span></a>
      <div className="rail-spacer" />
      <Avatar role="rail" />
    </nav>
  );
}

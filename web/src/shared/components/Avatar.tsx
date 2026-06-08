import { classNames } from '../lib/classNames';

export type AvatarRole =
  | 'logo'
  | 'rail'
  | 'emotion'
  | 'user'
  | 'error'
  | 'tool'
  | 'reasoning'
  | 'work'
  | 'memory';

const AVATAR_MAP: Record<AvatarRole, { emoji: string; className: string }> = {
  logo: { emoji: 'E', className: 'rail-logo' },
  rail: { emoji: '🐱', className: 'rail-avatar' },
  emotion: { emoji: '😉', className: 'msg-av' },
  user: { emoji: '😋', className: 'msg-av' },
  error: { emoji: '💢', className: 'msg-av' },
  tool: { emoji: '🧰', className: 'tool-av' },
  reasoning: { emoji: '💭', className: 'reasoning-av' },
  work: { emoji: '🐝', className: 'progress-av' },
  memory: { emoji: '🌙', className: 'memory-pipeline-av' },
};

export function Avatar({ role, className }: { role: AvatarRole; className?: string }) {
  const config = AVATAR_MAP[role];
  if (!config) return null;
  return (
    <div className={classNames(config.className, className)} aria-hidden="true">
      {config.emoji}
    </div>
  );
}

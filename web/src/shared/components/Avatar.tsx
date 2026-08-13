import type { ReactNode } from 'react';
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

/**
 * 24x24 stroke icons, matching the set already used by AppRail so the app has
 * one icon language. System roles (tool / reasoning / work / memory / error)
 * used to be colour emoji, which on Windows render as Segoe UI Emoji glyphs and
 * sit visibly apart from every other icon in the UI.
 *
 * The two conversational roles keep a character on purpose — this is a
 * companion app, and a line icon there would read colder than intended. They
 * are told apart by their container instead (see .msg-av rules in app.css).
 */
function Icon({ children }: { children: ReactNode }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="avatar-icon">
      {children}
    </svg>
  );
}

const ICONS: Partial<Record<AvatarRole, ReactNode>> = {
  tool: (
    <Icon>
      <path d="M14.7 6.3a4 4 0 0 1 5.3 5.3l-8.4 8.4a2.1 2.1 0 0 1-3-3l8-8" />
      <path d="M3.5 9.5 8 5l2.5 2.5" />
    </Icon>
  ),
  reasoning: (
    <Icon>
      <path d="M9 18h6M10 21h4" />
      <path d="M12 3a6 6 0 0 0-3.5 10.9c.4.3.5.7.5 1.1h6c0-.4.1-.8.5-1.1A6 6 0 0 0 12 3Z" />
    </Icon>
  ),
  work: (
    <Icon>
      <rect x="3" y="7" width="18" height="13" rx="2" />
      <path d="M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2M3 12h18" />
    </Icon>
  ),
  memory: (
    <Icon>
      <path d="M12 3v18M5 8l14 8M19 8 5 16" />
      <circle cx="12" cy="12" r="9" />
    </Icon>
  ),
  error: (
    <Icon>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v6M12 16.5v.01" />
    </Icon>
  ),
  // Sits next to the "本地运行" label in the rail footer. Previously a 🐱,
  // which was one of the two loudest colours on an otherwise restrained page.
  rail: (
    <Icon>
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M8 20h8M12 16v4" />
    </Icon>
  ),
};

const GLYPHS: Partial<Record<AvatarRole, string>> = {
  logo: 'E',
  emotion: '😉',
  user: '😋',
};

const CLASS_NAMES: Record<AvatarRole, string> = {
  logo: 'rail-logo',
  rail: 'rail-avatar',
  emotion: 'msg-av',
  user: 'msg-av',
  error: 'msg-av',
  tool: 'tool-av',
  reasoning: 'reasoning-av',
  work: 'progress-av',
  memory: 'memory-pipeline-av',
};

export function Avatar({ role, className }: { role: AvatarRole; className?: string }) {
  const rootClass = CLASS_NAMES[role];
  if (!rootClass) return null;
  const icon = ICONS[role];
  const content = icon ?? GLYPHS[role];
  if (!content) return null;
  return (
    <div className={classNames(rootClass, Boolean(icon) && 'is-icon', className)} aria-hidden="true">
      {content}
    </div>
  );
}

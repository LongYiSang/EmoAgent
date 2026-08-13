import { classNames } from '../lib/classNames';
import type { Theme } from '../hooks/useTheme';

/**
 * One control shared by all four pages. The chat page used to own the only
 * theme switch in the app, which left the log and admin pages permanently light.
 */
export function ThemeToggle({ theme, onToggle, className }: {
  theme: Theme;
  onToggle: () => void;
  className?: string;
}) {
  const label = theme === 'dark' ? '切换到浅色模式' : '切换到深色模式';
  return (
    <button
      className={classNames('btn ghost theme-toggle', className)}
      id="theme-toggle"
      type="button"
      aria-label={label}
      title={label}
      onClick={onToggle}
    >
      {theme === 'dark' ? (
        <svg viewBox="0 0 24 24" aria-hidden="true" className="avatar-icon">
          <circle cx="12" cy="12" r="4.5" />
          <path d="M12 2v2.5M12 19.5V22M4.2 4.2l1.8 1.8M18 18l1.8 1.8M2 12h2.5M19.5 12H22M4.2 19.8 6 18M18 6l1.8-1.8" />
        </svg>
      ) : (
        <svg viewBox="0 0 24 24" aria-hidden="true" className="avatar-icon">
          <path d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5Z" />
        </svg>
      )}
    </button>
  );
}

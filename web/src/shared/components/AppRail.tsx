import { useCallback, useEffect, useRef, useState, type FocusEvent, type ReactNode } from 'react';
import { classNames } from '../lib/classNames';
import { Avatar } from './Avatar';

type RailKey = 'chat' | 'logs' | 'plugins' | 'admin';

const PIN_STORAGE_KEY = 'emoagent.rail.pinned';
/** Keep rail open across full page navigations until the pointer leaves. */
const BRIDGE_STORAGE_KEY = 'emoagent.rail.bridgeExpanded';

const NAV: Array<{
  key: RailKey;
  href: string;
  label: string;
  icon: ReactNode;
}> = [
  {
    key: 'chat',
    href: '/',
    label: '聊天',
    icon: (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <path d="M7.5 19.5 4 21l1.5-3.5A8.5 8.5 0 1 1 12 20.5a8.4 8.4 0 0 1-4.5-1Z" />
        <path d="M8.5 11h7M8.5 14h4.5" />
      </svg>
    ),
  },
  {
    key: 'logs',
    href: '/logs.html',
    label: '日志',
    icon: (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <path d="M8 6h12M8 12h12M8 18h12" />
        <path d="M4 6h.01M4 12h.01M4 18h.01" />
      </svg>
    ),
  },
  {
    key: 'plugins',
    href: '/plugins.html',
    label: '插件',
    icon: (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3" />
        <rect x="6" y="6" width="12" height="12" rx="2.5" />
      </svg>
    ),
  },
  {
    key: 'admin',
    href: '/admin.html',
    label: '配置',
    icon: (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <circle cx="12" cy="12" r="3" />
        <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9c.3.6.9 1 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z" />
      </svg>
    ),
  },
];

function writeLocal(key: string, value: boolean) {
  try {
    window.localStorage.setItem(key, value ? '1' : '0');
  } catch {
    // ignore
  }
}

function writeBridge(active: boolean) {
  try {
    if (active) window.sessionStorage.setItem(BRIDGE_STORAGE_KEY, '1');
    else window.sessionStorage.removeItem(BRIDGE_STORAGE_KEY);
  } catch {
    // ignore
  }
}

function readPinned(): boolean {
  try {
    return window.localStorage.getItem(PIN_STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

function readBridge(): boolean {
  try {
    return window.sessionStorage.getItem(BRIDGE_STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

export function AppRail({ active }: { active: RailKey }) {
  // Sync init avoids “collapsed → expanded” flash / animation after navigation.
  const [pinned, setPinned] = useState(() => (typeof window !== 'undefined' ? readPinned() : false));
  const [open, setOpen] = useState(() => (
    typeof window !== 'undefined' ? (readPinned() || readBridge()) : false
  ));
  const [motionReady, setMotionReady] = useState(false);
  const leaveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pointerInsideRef = useRef(false);

  const clearLeaveTimer = useCallback(() => {
    if (leaveTimerRef.current !== null) {
      clearTimeout(leaveTimerRef.current);
      leaveTimerRef.current = null;
    }
  }, []);

  useEffect(() => () => clearLeaveTimer(), [clearLeaveTimer]);

  // Enable width transitions only after the first painted frame.
  useEffect(() => {
    let frame2 = 0;
    const frame1 = window.requestAnimationFrame(() => {
      frame2 = window.requestAnimationFrame(() => setMotionReady(true));
    });
    return () => {
      window.cancelAnimationFrame(frame1);
      window.cancelAnimationFrame(frame2);
    };
  }, []);

  const expand = useCallback((opts?: { pin?: boolean; bridge?: boolean }) => {
    clearLeaveTimer();
    setOpen(true);
    if (opts?.pin) {
      setPinned(true);
      writeLocal(PIN_STORAGE_KEY, true);
    }
    if (opts?.bridge) writeBridge(true);
  }, [clearLeaveTimer]);

  const collapse = useCallback((opts?: { unpin?: boolean }) => {
    clearLeaveTimer();
    setOpen(false);
    writeBridge(false);
    if (opts?.unpin) {
      setPinned(false);
      writeLocal(PIN_STORAGE_KEY, false);
    }
  }, [clearLeaveTimer]);

  const handleMouseEnter = () => {
    pointerInsideRef.current = true;
    expand({ bridge: true });
  };

  const handleMouseLeave = () => {
    pointerInsideRef.current = false;
    clearLeaveTimer();
    leaveTimerRef.current = setTimeout(() => {
      leaveTimerRef.current = null;
      if (pointerInsideRef.current) return;
      writeBridge(false);
      if (!pinned) setOpen(false);
    }, 180);
  };

  const handleFocusCapture = () => {
    expand({ bridge: true });
  };

  const handleBlurCapture = (event: FocusEvent<HTMLElement>) => {
    const next = event.relatedTarget;
    if (next instanceof Node && event.currentTarget.contains(next)) return;
    if (pointerInsideRef.current) return;
    writeBridge(false);
    if (!pinned) setOpen(false);
  };

  const handlePinToggle = () => {
    if (pinned) {
      collapse({ unpin: true });
      return;
    }
    expand({ pin: true, bridge: true });
  };

  const handleNavClick = () => {
    // Full page navigation remounts the rail; keep it open until pointer leaves.
    writeBridge(true);
  };

  const expanded = open || pinned;

  return (
    <nav
      className={classNames(
        'rail',
        expanded ? 'is-expanded' : 'is-collapsed',
        pinned && 'is-pinned',
        motionReady && 'is-motion-ready',
      )}
      aria-label="主导航"
      data-expanded={expanded ? 'true' : 'false'}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      onFocusCapture={handleFocusCapture}
      onBlurCapture={handleBlurCapture}
    >
      <a className="rail-brand" href="/" aria-label="EmoAgent 首页" onClick={handleNavClick}>
        <span className="rail-logo" aria-hidden="true">E</span>
        <span className="rail-brand-text">
          <span className="rail-brand-title">EmoAgent</span>
          <span className="rail-brand-sub">本地控制台</span>
        </span>
      </a>

      <div className="rail-nav">
        {NAV.map(item => (
          <a
            key={item.key}
            className={classNames('rail-btn', active === item.key && 'active')}
            href={item.href}
            aria-current={active === item.key ? 'page' : undefined}
            aria-label={item.label}
            title={item.label}
            onClick={handleNavClick}
          >
            <span className="rail-btn-icon">{item.icon}</span>
            <span className="rail-btn-label">{item.label}</span>
            <span className="rail-tooltip" aria-hidden="true">{item.label}</span>
          </a>
        ))}
      </div>

      <div className="rail-spacer" />

      <div className="rail-footer">
        <Avatar role="rail" />
        <div className="rail-footer-meta">
          <span className="rail-footer-title">本地运行</span>
          <span className="rail-footer-sub">Agent Console</span>
        </div>
        <button
          type="button"
          className="rail-pin"
          onClick={handlePinToggle}
          aria-pressed={pinned}
          aria-label={pinned ? '取消固定侧栏' : '固定展开侧栏'}
          title={pinned ? '取消固定（离开后自动收起）' : '固定展开'}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" className={pinned ? 'is-filled' : undefined}>
            <path d="M16 3v6l2 2v2h-5v7l-1 1-1-1v-7H6v-2l2-2V3h8Z" />
          </svg>
        </button>
      </div>
    </nav>
  );
}

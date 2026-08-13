import { useCallback, useEffect, useState } from 'react';

export type Theme = 'light' | 'dark';

/** Shared by every entry point so a choice made on one page holds on all of them. */
const THEME_STORAGE_KEY = 'emoagent-theme';

function readStoredTheme(): Theme | null {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY);
    return stored === 'dark' || stored === 'light' ? stored : null;
  } catch {
    return null;
  }
}

function currentDocumentTheme(): Theme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light';
}

/**
 * Theme state for a page. This used to live inside ChatApp, so the chat page was
 * the only one that could go dark — while the log and admin pages, where a local
 * operator actually spends their time, were locked to white.
 *
 * The inline script in each HTML entry has already resolved and applied the
 * theme before first paint; this hook adopts that value, keeps following the OS
 * until the user makes an explicit choice, and persists the choice once made.
 */
export function useTheme() {
  const [theme, setTheme] = useState<Theme>(currentDocumentTheme);
  const [explicit, setExplicit] = useState(() => readStoredTheme() !== null);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    if (!explicit) return;
    try {
      localStorage.setItem(THEME_STORAGE_KEY, theme);
    } catch {
      // localStorage unavailable — theme just won't persist
    }
  }, [theme, explicit]);

  // Track the OS for as long as the user has not overridden it.
  useEffect(() => {
    if (explicit || typeof window === 'undefined' || !window.matchMedia) return;
    const query = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = (event: MediaQueryListEvent) => setTheme(event.matches ? 'dark' : 'light');
    query.addEventListener('change', onChange);
    return () => query.removeEventListener('change', onChange);
  }, [explicit]);

  const toggleTheme = useCallback(() => {
    setExplicit(true);
    setTheme(current => (current === 'dark' ? 'light' : 'dark'));
  }, []);

  return { theme, toggleTheme };
}

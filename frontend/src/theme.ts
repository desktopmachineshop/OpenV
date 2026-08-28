import { useEffect, useState } from 'react';

// Theme manager: three states — 'system' (default, no data-theme attribute so
// the @media (prefers-color-scheme) rules decide), 'light' and 'dark' (explicit
// data-theme attribute on <html> that overrides the media query).
//
// The explicit choice is persisted in localStorage under THEME_STORAGE_KEY and
// re-applied before first paint by an inline script in public/index.html (kept
// in sync with this module) to avoid a flash of the wrong theme.

export type ThemePreference = 'system' | 'light' | 'dark';

export const THEME_STORAGE_KEY = 'openv-theme';

const listeners = new Set<() => void>();

const notify = () => listeners.forEach((fn) => fn());

// React to OS-level scheme changes while in 'system' mode so subscribers
// (e.g. the cytoscape canvas) re-read their colors.
const media =
  typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(prefers-color-scheme: dark)')
    : null;
if (media) {
  const onChange = () => {
    if (getThemePreference() === 'system') notify();
  };
  if (typeof media.addEventListener === 'function') media.addEventListener('change', onChange);
  else if (typeof (media as any).addListener === 'function') (media as any).addListener(onChange);
}

export function getThemePreference(): ThemePreference {
  try {
    const v = localStorage.getItem(THEME_STORAGE_KEY);
    if (v === 'light' || v === 'dark') return v;
  } catch {
    // localStorage unavailable (private mode, blocked) — fall through.
  }
  return 'system';
}

/** Apply the preference to <html> without persisting it. */
export function applyThemePreference(pref: ThemePreference): void {
  const root = document.documentElement;
  if (pref === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', pref);
}

/** Persist and apply an explicit preference ('system' clears the override). */
export function setThemePreference(pref: ThemePreference): void {
  try {
    if (pref === 'system') localStorage.removeItem(THEME_STORAGE_KEY);
    else localStorage.setItem(THEME_STORAGE_KEY, pref);
  } catch {
    // Persistence is best-effort; still apply for this session.
  }
  applyThemePreference(pref);
  notify();
}

/** Whether the effective (resolved) theme is dark right now. */
export function isDarkTheme(): boolean {
  const pref = getThemePreference();
  if (pref === 'dark') return true;
  if (pref === 'light') return false;
  return !!media && media.matches;
}

/** Read a CSS custom property's current computed value (e.g. cssVar('--text')). */
export function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

export function subscribeTheme(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Hook: the stored preference plus a setter. */
export function useThemePreference(): [ThemePreference, (pref: ThemePreference) => void] {
  const [pref, setPref] = useState<ThemePreference>(getThemePreference);
  useEffect(() => subscribeTheme(() => setPref(getThemePreference())), []);
  return [pref, setThemePreference];
}

/**
 * Hook: a counter that bumps whenever the effective theme may have changed.
 * Use it as an effect dependency to re-run theme-dependent imperative code
 * (canvas renderers and other places CSS variables aren't resolved live).
 */
export function useThemeVersion(): number {
  const [version, setVersion] = useState(0);
  useEffect(() => subscribeTheme(() => setVersion((v) => v + 1)), []);
  return version;
}

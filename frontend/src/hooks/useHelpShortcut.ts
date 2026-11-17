import { useEffect, useRef } from 'react';

// The keyboard way to the help panel: ? (Shift + / on most layouts) or F1,
// from anywhere on a page with a help panel, toggles it. Part of the compact
// layout (the compact-layout feature key); the manual lists it under
// Keyboard shortcuts.
//
// ? is also a character people type, so it is ignored while the focus is in
// a field, a text area, a select or editable content. F1 is never typed, so
// it works there too, and its browser default (the browser's own help) is
// suppressed. A key pressed with Ctrl, Alt or Meta belongs to something else.

/** The part of a KeyboardEvent the shortcut reads. */
export interface ShortcutKey {
  key: string;
  ctrlKey?: boolean;
  altKey?: boolean;
  metaKey?: boolean;
  defaultPrevented?: boolean;
  target?: EventTarget | null;
}

const EDITABLE = 'input, textarea, select, [contenteditable=""], [contenteditable="true"]';

/** Whether the event happened where text is typed. */
export const typingIn = (target: EventTarget | null | undefined): boolean =>
  typeof Element !== 'undefined' && target instanceof Element && target.closest(EDITABLE) !== null;

/** Whether a key press asks for the help panel. */
export const isHelpShortcut = (e: ShortcutKey): boolean => {
  if (e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey) return false;
  if (e.key === 'F1') return true;
  return e.key === '?' && !typingIn(e.target);
};

/**
 * While enabled, ? and F1 call onToggle. The latest onToggle is used, so the
 * caller may pass a fresh function every render.
 */
export const useHelpShortcut = (enabled: boolean, onToggle: () => void): void => {
  const latest = useRef(onToggle);
  latest.current = onToggle;
  useEffect(() => {
    if (!enabled) return undefined;
    const onKey = (e: KeyboardEvent) => {
      if (!isHelpShortcut(e)) return;
      e.preventDefault();
      latest.current();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [enabled]);
};

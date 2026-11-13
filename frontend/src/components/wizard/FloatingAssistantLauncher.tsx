import React, { useState } from 'react';

const TUCKED_KEY = 'openv-assistant-tucked';

const loadTucked = (): boolean => {
  try {
    return window.localStorage.getItem(TUCKED_KEY) === '1';
  } catch {
    return false;
  }
};

const saveTucked = (tucked: boolean): void => {
  try {
    if (tucked) window.localStorage.setItem(TUCKED_KEY, '1');
    else window.localStorage.removeItem(TUCKED_KEY);
  } catch {
    // A browser that keeps nothing still tucks it away for this visit.
  }
};

interface LauncherProps {
  /** The corner the assistant keeps to. */
  left: boolean;
  bottom: string;
  onOpen: () => void;
  launcherRef: React.Ref<HTMLButtonElement>;
}

/**
 * The assistant's button in the corner of the page, and the way to put it
 * out of the way: the small mark at its side tucks it into a tab on the
 * edge of the window, which brings it back. The browser remembers which,
 * so a reader who tucked it away is not shown it again on the next page.
 * Alt+A opens the assistant either way.
 */
export const FloatingAssistantLauncher: React.FC<LauncherProps> = ({ left, bottom, onOpen, launcherRef }) => {
  const [tucked, setTucked] = useState(loadTucked);
  const tuck = (next: boolean) => {
    setTucked(next);
    saveTucked(next);
  };
  const side = left ? 'left' : 'right';
  if (tucked) {
    return (
      <button
        ref={launcherRef}
        type="button"
        data-assistant-tab
        onClick={() => tuck(false)}
        aria-label="Show the V&V Assistant button"
        title="Show the V&V Assistant (Alt+A opens it)"
        style={{
          position: 'fixed',
          [side]: 0,
          bottom,
          width: 22,
          minHeight: 56,
          padding: 0,
          border: 'none',
          borderRadius: left ? '0 8px 8px 0' : '8px 0 0 8px',
          background: 'var(--accent)',
          color: 'var(--accent-fg)',
          fontSize: 13,
          boxShadow: '0 2px 8px rgba(0,0,0,0.25)',
          cursor: 'pointer',
          zIndex: 940,
        }}
      >
        <span aria-hidden="true">✦</span>
      </button>
    );
  }
  return (
    <span
      style={{
        position: 'fixed',
        [side]: 16,
        bottom,
        display: 'inline-flex',
        flexDirection: left ? 'row-reverse' : 'row',
        alignItems: 'stretch',
        borderRadius: 22,
        background: 'var(--accent)',
        color: 'var(--accent-fg)',
        boxShadow: '0 4px 12px rgba(0,0,0,0.25)',
        zIndex: 940,
      }}
    >
      <button
        ref={launcherRef}
        type="button"
        onClick={onOpen}
        aria-label="Open the V&V Assistant"
        title="Open the V&V Assistant (Alt+A)"
        aria-keyshortcuts="Alt+A"
        data-help-key="guided.assistant"
        style={{ minHeight: 44, padding: left ? '0 16px 0 6px' : '0 6px 0 16px', border: 'none', background: 'none', color: 'inherit', fontSize: 14, fontWeight: 600, cursor: 'pointer', borderRadius: 22 }}
      >
        <span aria-hidden="true">✦</span> Assistant
      </button>
      <button
        type="button"
        data-assistant-tuck
        onClick={() => tuck(true)}
        aria-label="Tuck the V&V Assistant button away"
        title="Tuck away: it folds into a tab on the edge of the window"
        style={{ minWidth: 30, padding: 0, border: 'none', borderLeft: left ? 'none' : '1px solid color-mix(in srgb, var(--accent-fg) 35%, transparent)', borderRight: left ? '1px solid color-mix(in srgb, var(--accent-fg) 35%, transparent)' : 'none', background: 'none', color: 'inherit', fontSize: 14, cursor: 'pointer', borderRadius: left ? '22px 0 0 22px' : '0 22px 22px 0' }}
      >
        <span aria-hidden="true">{left ? '‹' : '›'}</span>
      </button>
    </span>
  );
};

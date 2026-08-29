import React from 'react';
import { ThemePreference, useThemePreference } from '../theme';

const OPTIONS: { value: ThemePreference; label: string; hint: string }[] = [
  { value: 'system', label: 'System', hint: 'Follow the operating system setting' },
  { value: 'light', label: 'Light', hint: 'Always use the light theme' },
  { value: 'dark', label: 'Dark', hint: 'Always use the dark theme' },
];

// Small System / Light / Dark segmented control. The choice is stored in
// localStorage and applied as data-theme on <html> (see src/theme.ts).
export const ThemeSwitcher: React.FC = () => {
  const [pref, setPref] = useThemePreference();

  return (
    <div
      role="radiogroup"
      aria-label="Theme"
      style={{
        display: 'inline-flex',
        border: '1px solid var(--neutral-mid)',
        borderRadius: 6,
        overflow: 'hidden',
      }}
    >
      {OPTIONS.map((opt) => {
        const active = pref === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={active}
            title={opt.hint}
            onClick={() => setPref(opt.value)}
            style={{
              width: 'auto',
              padding: '6px 14px',
              fontSize: 12,
              fontWeight: active ? 700 : 400,
              border: 'none',
              cursor: 'pointer',
              background: active ? 'var(--accent)' : 'var(--surface)',
              color: active ? 'var(--accent-fg)' : 'var(--text)',
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
};

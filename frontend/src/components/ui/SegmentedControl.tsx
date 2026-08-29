import React from 'react';

export interface SegmentedOption<T extends string> {
  value: T;
  label: React.ReactNode;
  title?: string;
  /** Per-option active color override (falls back to activeColor). */
  activeColor?: string;
}

interface SegmentedControlProps<T extends string> {
  options: SegmentedOption<T>[];
  value: T;
  onChange: (value: T) => void;
  /** Background of the selected segment (default accent blue). */
  activeColor?: string;
  style?: React.CSSProperties;
  'aria-label'?: string;
}

/**
 * Pill/toggle group used for view switches, filters and kind pickers —
 * visually consistent with the existing bordered tab styling.
 */
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  activeColor = 'var(--accent)',
  style,
  'aria-label': ariaLabel,
}: SegmentedControlProps<T>) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      style={{
        display: 'inline-flex',
        border: '1px solid var(--border)',
        borderRadius: 4,
        overflow: 'hidden',
        ...style,
      }}
    >
      {options.map((opt) => {
        const active = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            title={opt.title}
            aria-pressed={active}
            onClick={() => onChange(opt.value)}
            style={{
              padding: '6px 14px',
              border: 'none',
              cursor: 'pointer',
              fontSize: 13,
              background: active ? opt.activeColor || activeColor : 'var(--surface)',
              color: active ? '#fff' : 'var(--text)',
              fontWeight: active ? 600 : 400,
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}

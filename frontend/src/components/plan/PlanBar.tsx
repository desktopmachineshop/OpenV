// The bar of a task on a time scale, wherever the plan draws one: a row of
// the Gantt, the Timeline's band and a project's line of All projects.
// One component draws them all, so a late task, a parent, the critical
// path and the selection read the same on every chart; a chart decides
// only where the bar goes and what a press on it does.
import React from 'react';
import { ellipsis } from '../ui';
import { BAR_COLORS, type BarTone } from './planModel';

export interface PlanBarProps {
  left: number;
  width: number;
  top: number;
  height: number;
  /** The task's state: done, late, under way or not started. */
  tone: BarTone;
  /** How much of the bar is filled, 0 to 100. */
  progress: number;
  /** A parent, whose dates span its sub-tasks: drawn in the neutral tone. */
  summary?: boolean;
  /** Drawn as on the critical path: an amber ring. */
  critical?: boolean;
  selected?: boolean;
  /** What the bar says to a pointer and to a screen reader. */
  label: string;
  /** The task's name written on the bar, where the chart has no outline beside it to name the row. */
  text?: string;
  cursor?: React.CSSProperties['cursor'];
  /** The press, the drag and the data attributes the chart puts on the bar. */
  button?: React.ButtonHTMLAttributes<HTMLButtonElement> & Record<`data-${string}`, string | boolean | undefined>;
  /** Drawn inside the bar, over its fill: the Gantt's resize handles. */
  children?: React.ReactNode;
}

export const PlanBar: React.FC<PlanBarProps> = ({ left, width, top, height, tone, progress, summary, critical, selected, label, text, cursor = 'pointer', button, children }) => {
  const colors = BAR_COLORS[tone];
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      {...button}
      style={{
        position: 'absolute',
        left,
        width,
        top,
        height,
        boxSizing: 'border-box',
        padding: 0,
        borderRadius: summary ? 2 : 4,
        border: `1px solid ${summary ? 'var(--text-secondary)' : colors.border}`,
        background: summary ? 'var(--neutral-soft)' : colors.track,
        boxShadow: critical ? '0 0 0 2px var(--warning)' : selected ? '0 0 0 2px var(--tint-blue-border)' : undefined,
        color: 'var(--text)',
        textAlign: 'left',
        cursor,
        overflow: 'visible',
        ...button?.style,
      }}
    >
      <span
        aria-hidden
        style={{
          display: 'block',
          height: '100%',
          width: `${progress}%`,
          borderRadius: 'inherit',
          background: summary ? 'var(--text-secondary)' : colors.fill,
          // A parent's fill is quieter than a task's, and quieter still under its name.
          opacity: summary ? (text ? 0.35 : 0.85) : 1,
        }}
      />
      {text !== undefined && (
        <span style={{ ...ellipsis, position: 'absolute', inset: 0, padding: '0 6px', fontSize: 12, lineHeight: `${height - 2}px`, fontWeight: summary ? 600 : 400 }}>
          <span style={{ background: 'color-mix(in srgb, var(--surface) 70%, transparent)', borderRadius: 2, padding: '0 2px' }}>{text}</span>
        </span>
      )}
      {children}
    </button>
  );
};

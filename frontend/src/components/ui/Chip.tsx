import React from 'react';

// The one chip of the app's status vocabulary: a pale tint with a matching
// border and dark text, so a status reads as a word first and a colour second
// (the design system: status is "a word plus a color, never color alone").
// Pages used to draw their own solid pills in white on a status fill, which
// fell under 4.5:1 for most fills and shouted on a dense table.
//
// Tones are fixed meanings: success = pass / approved / completed, danger =
// fail / aborted, warning = blocked / suspect, info = in review / running /
// in progress, neutral = draft / not-run / unknown, agent = AI proposals and
// agent work. toneFor maps the words the app's vocabularies use onto them.

export type ChipTone = 'success' | 'danger' | 'warning' | 'info' | 'neutral' | 'agent';

const TONES: Record<ChipTone, { bg: string; border: string; fg: string }> = {
  success: { bg: 'var(--tint-green)', border: 'var(--tint-green-border)', fg: 'var(--success-text)' },
  danger: { bg: 'var(--tint-red)', border: 'var(--tint-red-border)', fg: 'var(--danger-strong)' },
  warning: { bg: 'var(--tint-yellow)', border: 'var(--tint-yellow-border)', fg: 'var(--warning-text)' },
  info: { bg: 'var(--tint-blue)', border: 'var(--tint-blue-border)', fg: 'var(--accent-text)' },
  neutral: { bg: 'var(--neutral-soft)', border: 'var(--border)', fg: 'var(--text-secondary)' },
  agent: { bg: 'var(--tint-purple)', border: 'var(--tint-purple-border)', fg: 'var(--purple-strong)' },
};

const WORDS: Record<string, ChipTone> = {
  pass: 'success',
  passed: 'success',
  approved: 'success',
  completed: 'success',
  succeeded: 'success',
  verified: 'success',
  'verified-manually': 'success',
  covered: 'success',
  good: 'success',
  done: 'success',
  fail: 'danger',
  failed: 'danger',
  aborted: 'danger',
  rejected: 'danger',
  error: 'danger',
  poor: 'danger',
  blocked: 'warning',
  suspect: 'warning',
  fair: 'warning',
  'method-missing': 'warning',
  'in-review': 'info',
  'in review': 'info',
  review: 'info',
  running: 'info',
  'in-progress': 'info',
  'in progress': 'info',
  queued: 'info',
  pending: 'info',
  agent: 'agent',
  proposal: 'agent',
  proposed: 'agent',
};

/** The tone of a status word; anything unknown (draft, not-run, uncovered) is neutral. */
export const toneFor = (status: string | null | undefined): ChipTone =>
  WORDS[(status || '').toLowerCase().trim()] ?? 'neutral';

/**
 * The tone of a pill a page used to fill with a theme colour, so the old
 * solid pills move onto the tinted chip without each page relearning the
 * vocabulary: the success, danger and warning fills keep their meaning, the
 * accent is info, purple is agent work, anything else neutral.
 */
export const toneForColor = (color: string): ChipTone => {
  if (/--success/.test(color)) return 'success';
  if (/--danger/.test(color)) return 'danger';
  if (/--warning/.test(color)) return 'warning';
  if (/--purple/.test(color)) return 'agent';
  if (/--accent/.test(color)) return 'info';
  return 'neutral';
};

/** The tone's colours, for a page that draws its own element in the chip's colours. */
export const toneColors = (tone: ChipTone) => TONES[tone];

/** The chip's inline style, for a page that needs it on an element of its own. */
export const chipStyle = (tone: ChipTone, size: 'sm' | 'md' = 'sm'): React.CSSProperties => ({
  display: 'inline-flex',
  alignItems: 'center',
  gap: 4,
  padding: size === 'sm' ? '1px 8px' : '2px 10px',
  borderRadius: 10,
  border: `1px solid ${TONES[tone].border}`,
  background: TONES[tone].bg,
  color: TONES[tone].fg,
  fontSize: 12,
  fontWeight: 700,
  lineHeight: 1.5,
  whiteSpace: 'nowrap',
  verticalAlign: 'middle',
});

interface ChipProps {
  /** The tone; omitted, it comes from the status word in children (toneFor). */
  tone?: ChipTone;
  size?: 'sm' | 'md';
  title?: string;
  className?: string;
  children: React.ReactNode;
}

export const Chip: React.FC<ChipProps> = ({ tone, size = 'sm', title, className, children }) => {
  const resolved = tone ?? (typeof children === 'string' ? toneFor(children) : 'neutral');
  return (
    <span className={className ? `ov-chip ${className}` : 'ov-chip'} title={title} style={chipStyle(resolved, size)}>
      {children}
    </span>
  );
};

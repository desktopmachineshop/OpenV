// The plan in a baseline comparison: where the project's plan stood on
// each side, so the same two moments answer how far the project got as
// well as what changed in its definition. A baseline records the plan when
// it is taken; the working copy's side is the plan as it stands.
import React from 'react';
import type { BaselinePlan } from '../api/client';
import { Chip, eyebrowStyle, mutedStyle, panelStyle } from '../components/ui';
import { shortDate, toDay } from '../components/plan/ganttScale';

const today = () => new Date().toISOString().slice(0, 10);

/** "3 days later", "2 days earlier", or nothing when a date did not move. */
export const dateShift = (before: string | null, after: string | null): string => {
  if (!before || !after || before === after) return '';
  const days = toDay(after) - toDay(before);
  return `${Math.abs(days)} ${Math.abs(days) === 1 ? 'day' : 'days'} ${days > 0 ? 'later' : 'earlier'}`;
};

export interface MilestoneChange {
  key: string;
  name: string;
  /** What happened between the two sides, in words; empty when nothing did. */
  change: string;
  tone: 'success' | 'danger' | 'info' | 'neutral';
}

/** Each milestone of either side, with what changed about it between them. */
export const milestoneChanges = (before: BaselinePlan, after: BaselinePlan, now = today()): MilestoneChange[] => {
  const was = new Map(before.milestones.map((m) => [m.key, m]));
  const out: MilestoneChange[] = after.milestones.map((m) => {
    const old = was.get(m.key);
    was.delete(m.key);
    if (!old) return { key: m.key, name: m.name, change: 'new', tone: 'info' };
    const parts: string[] = [];
    let tone: MilestoneChange['tone'] = 'neutral';
    if (!old.met && m.met) (parts.push('met'), (tone = 'success'));
    if (old.met && !m.met) (parts.push('no longer met'), (tone = 'danger'));
    const shift = dateShift(old.date, m.date);
    if (shift) {
      parts.push(`${shortDate(old.date, now)} → ${shortDate(m.date, now)}, ${shift}`);
      if (tone === 'neutral') tone = shift.endsWith('later') ? 'danger' : 'success';
    } else if (old.date !== m.date) parts.push(m.date ? `dated ${shortDate(m.date, now)}` : 'no date now');
    if (old.progress !== m.progress && !parts.includes('met')) parts.push(`${old.progress}% → ${m.progress}%`);
    return { key: m.key, name: m.name, change: parts.join(' · '), tone };
  });
  for (const m of was.values()) out.push({ key: m.key, name: m.name, change: 'removed', tone: 'danger' });
  return out;
};

const figure: React.CSSProperties = { display: 'flex', flexDirection: 'column', gap: 2, minWidth: 110 };

const Figure: React.FC<{ label: string; before?: string; after: string; note?: string; bad?: boolean }> = ({ label, before, after, note, bad }) => (
  <div style={figure}>
    <span style={eyebrowStyle}>{label}</span>
    <b style={{ fontSize: 15 }}>
      {before !== undefined && before !== after ? (
        <>
          <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>{before} → </span>
          {after}
        </>
      ) : (
        after
      )}
    </b>
    {note && <span style={{ fontSize: 12, color: bad ? 'var(--danger-strong)' : 'var(--success-text)' }}>{note}</span>}
  </div>
);

/**
 * The plan between the two sides of a comparison. With a plan on both it
 * shows each figure before and after; with one only on the later side
 * (the earlier baseline was taken before plans were recorded) it shows
 * that side alone, and says so. Nothing when neither side has a plan.
 */
export const PlanChange: React.FC<{ before?: BaselinePlan; after?: BaselinePlan; fromName: string; toName: string }> = ({ before, after, fromName, toName }) => {
  const at = after || before;
  if (!at) return null;
  const now = today();
  const both = before && after;
  const gain = both ? after.progress - before.progress : 0;
  const shift = both ? dateShift(before.end, after.end) : '';
  const moved = both ? milestoneChanges(before, after, now).filter((m) => m.change) : [];
  const count = (p: BaselinePlan) => `${p.done}/${p.tasks}`;
  return (
    <section aria-label="Plan" data-help-key="baselines.plan" style={{ ...panelStyle, padding: 12, display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 8 }}>
        <h2 style={{ margin: 0, fontSize: 14, fontWeight: 700 }}>Plan</h2>
        <span style={mutedStyle}>
          {both
            ? `where the plan stood at ${fromName}, and at ${toName}`
            : after
              ? `${fromName} did not record the plan: this is the plan at ${toName}`
              : `${toName} did not record the plan: this is the plan at ${fromName}`}
        </span>
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '12px 24px' }}>
        <Figure label="Progress" before={both ? `${before.progress}%` : undefined} after={`${at.progress}%`} note={gain ? `${gain > 0 ? '+' : ''}${gain} points` : undefined} bad={gain < 0} />
        <Figure label="Tasks done" before={both ? count(before) : undefined} after={count(at)} />
        <Figure label="Late" before={both ? String(before.late) : undefined} after={String(at.late)} note={both && after.late !== before.late ? (after.late > before.late ? 'more late' : 'fewer late') : undefined} bad={both ? after.late > before.late : false} />
        <Figure label="Finish" before={both ? shortDate(before.end, now) || 'Not scheduled' : undefined} after={shortDate(at.end, now) || 'Not scheduled'} note={shift || undefined} bad={shift.endsWith('later')} />
      </div>
      {moved.length > 0 && (
        <ul aria-label="Milestones that changed" style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13 }}>
          {moved.map((m) => (
            <li key={m.key} style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
              <span aria-hidden>◆</span>
              <b>{m.name}</b>
              <Chip tone={m.tone}>{m.change}</Chip>
            </li>
          ))}
        </ul>
      )}
      {both && moved.length === 0 && after.milestones.length > 0 && <span style={mutedStyle}>No milestone moved.</span>}
    </section>
  );
};

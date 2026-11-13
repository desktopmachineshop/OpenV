import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { BaselinePlan } from '../api/client';
import type { BaselineTaskChange } from '../api/client';
import { PlanChange, dateShift, milestoneChanges, taskLines } from './BaselinesPagePlan';

// The plan in a baseline comparison: one baseline records the definition
// and the plan together, so two of them say how far the project got as
// well as what changed.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const plan = (extra: Partial<BaselinePlan> = {}): BaselinePlan => ({
  progress: 30,
  tasks: 4,
  done: 1,
  late: 0,
  start: '2026-10-01',
  end: '2026-11-10',
  milestones: [
    { key: 'm1', name: 'Definition complete', kind: 'milestone', date: '2026-10-15', progress: 50, met: false },
    { key: 'r1', name: 'V1.0', kind: 'release', date: '2026-12-01', progress: 0, met: false },
  ],
  ...extra,
});

let container: HTMLDivElement;
let root: Root;

const draw = (before?: BaselinePlan, after?: BaselinePlan) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => root.render(<PlanChange before={before} after={after} fromName="Gate 1" toName="Working copy" />));
};

afterEach(() => {
  if (!container?.isConnected) return;
  act(() => root.unmount());
  container.remove();
});

describe('the plan between two baselines', () => {
  it('says how far a date moved, and which way', () => {
    expect(dateShift('2026-11-10', '2026-11-13')).toBe('3 days later');
    expect(dateShift('2026-11-10', '2026-11-09')).toBe('1 day earlier');
    expect(dateShift('2026-11-10', '2026-11-10')).toBe('');
    expect(dateShift(null, '2026-11-10')).toBe('');
  });

  it('names what happened to each milestone', () => {
    const after = plan({
      milestones: [
        { key: 'm1', name: 'Definition complete', kind: 'milestone', date: '2026-10-15', progress: 100, met: true },
        { key: 'r1', name: 'V1.0', kind: 'release', date: '2026-12-08', progress: 0, met: false },
        { key: 'm2', name: 'Pilot', kind: 'milestone', date: null, progress: 0, met: false },
      ],
    });
    expect(milestoneChanges(plan(), after, '2026-11-13').map((m) => [m.name, m.change, m.tone])).toEqual([
      ['Definition complete', 'met', 'success'],
      ['V1.0', '1 Dec → 8 Dec, 7 days later', 'danger'],
      ['Pilot', 'new', 'info'],
    ]);
    expect(milestoneChanges(after, plan()).find((m) => m.key === 'm2')?.change).toBe('removed');
    expect(milestoneChanges(plan(), plan()).every((m) => m.change === '')).toBe(true);
  });

  it('shows each figure before and after, and the milestones that changed', () => {
    draw(plan(), plan({ progress: 55, done: 3, late: 1, end: '2026-11-17', milestones: [plan().milestones[0], { ...plan().milestones[1], date: '2026-12-08' }] }));
    const text = container.textContent || '';
    expect(text).toContain('where the plan stood at Gate 1, and at Working copy');
    expect(text).toContain('30% → 55%');
    expect(text).toContain('+25 points');
    expect(text).toContain('1/4 → 3/4');
    expect(text).toContain('more late');
    expect(text).toContain('7 days later');
    expect(Array.from(container.querySelectorAll('li')).map((li) => li.querySelector('b')?.textContent)).toEqual(['V1.0']);
  });

  it('reads each changed task in words, the ones that slipped furthest first', () => {
    const t = (id: string, change: BaselineTaskChange['change'], extra: Partial<BaselineTaskChange> = {}): BaselineTaskChange => ({
      id,
      title: id,
      change,
      before: { id, start: '2026-11-02', end: '2026-11-10', progress: 10 },
      after: { id, start: '2026-11-02', end: '2026-11-10', progress: 40 },
      start_shift_days: 0,
      finish_shift_days: 0,
      ...extra,
    });
    const lines = taskLines(
      [
        t('Paint', 'progress'),
        t('Weld', 'moved', { finish_shift_days: 3, after: { id: 'Weld', start: '2026-11-02', end: '2026-11-13' } }),
        t('Wire', 'finished'),
        t('Ship', 'moved', { start_shift_days: 9, finish_shift_days: 9, after: { id: 'Ship', start: '2026-11-11', end: '2026-11-19' } }),
        t('Early', 'moved', { finish_shift_days: -2, after: { id: 'Early', start: '2026-11-02', end: '2026-11-08' } }),
        t('New', 'added', { before: null }),
        { ...t('', 'removed', { after: null }), id: 'gone' },
      ],
      '2026-11-13',
    );
    expect(lines.map((l) => [l.title, l.change, l.tone])).toEqual([
      ['Ship', 'ends 9 days later (10 Nov → 19 Nov)', 'danger'],
      ['Weld', 'ends 3 days later (10 Nov → 13 Nov)', 'danger'],
      ['Paint', '10% → 40%', 'neutral'],
      ['Wire', 'done', 'success'],
      ['Early', 'ends 2 days earlier (10 Nov → 8 Nov)', 'success'],
      ['New', 'added', 'info'],
      ['A task since deleted', 'removed', 'danger'],
    ]);
  });

  it('lists the changed tasks under the figures, and folds a long list', () => {
    const many: BaselineTaskChange[] = Array.from({ length: 10 }, (_, i) => ({ id: `t${i}`, title: `Task ${i}`, change: 'added', before: null, after: { id: `t${i}`, start: null, end: null }, start_shift_days: 0, finish_shift_days: 0 }));
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    act(() => root.render(<PlanChange before={plan()} after={plan()} tasks={many} fromName="Gate 1" toName="Working copy" />));
    const items = () => container.querySelectorAll('ul[aria-label="Tasks that changed"] li');
    expect(items()).toHaveLength(8);
    const more = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Show all 10')!;
    act(() => more.click());
    expect(items()).toHaveLength(10);
    act(() => root.unmount());
    container.remove();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    act(() => root.render(<PlanChange before={plan()} after={plan()} tasks={[]} fromName="Gate 1" toName="Working copy" />));
    expect(container.textContent).toContain('Every task reads as it did.');
  });

  it('shows the one side that has a plan, and nothing when neither has', () => {
    draw(undefined, plan());
    expect(container.textContent).toContain('Gate 1 did not record the plan: this is the plan at Working copy');
    expect(container.textContent).toContain('30%');
    expect(container.textContent).not.toContain('→');
    act(() => root.unmount());
    container.remove();
    draw(undefined, undefined);
    expect(container.querySelector('section')).toBeNull();
  });
});

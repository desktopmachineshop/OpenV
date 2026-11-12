import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { PlanTask, ProjectPlan } from '../../api/client';
import { lanes, PlanTimeline, type Bar } from './PlanLines';
import { calendarRange, PlanMonth } from './PlanMonth';
import { PlanNetwork } from './PlanNetwork';
import { PlanUsage } from './PlanUsage';

// The views of the plan other than the Gantt: each answers the Critical path
// button, the timeline keeps the critical path on its top row, and the
// calendar shows as much as the time scale asks for.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const mount = async (ui: React.ReactElement) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(ui));
};

afterEach(() => {
  // The pure tests mount nothing.
  if (!container?.isConnected) return;
  act(() => root.unmount());
  container.remove();
});

const click = (el: Element | null | undefined) =>
  act(async () => {
    el!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });

const task = (id: string, title: string, start: string, end: string, extra: Partial<PlanTask> = {}): PlanTask =>
  ({
    id,
    title,
    wbs: id,
    depth: 0,
    span_start: start,
    span_end: end,
    progress: 0,
    done: false,
    late: false,
    critical: false,
    critical_subtasks: false,
    subtasks_total: 0,
    float_days: null,
    elapsed: false,
    relationships: [],
    drives: [],
    assignments: [],
    ...extra,
  }) as unknown as PlanTask;

const plan = {
  project_id: 'p1',
  working_days: [1, 2, 3, 4, 5],
  holidays: [],
  today: '2026-11-12',
  start: '2026-11-02',
  end: '2026-12-20',
  milestones: [],
  resources: [{ type: 'user', id: 'u1', name: 'Sam', capacity: 100 }],
  tasks: [
    task('1', 'Paint', '2026-11-02', '2026-11-13'),
    task('2', 'Order steel', '2026-11-09', '2026-11-20', { critical: true, assignments: [{ type: 'user', id: 'u1', units: 100 }] as never }),
    task('3', 'Weld', '2026-11-23', '2026-12-04', { critical: true, relationships: [{ task_id: '2', type: 'FS', conflict: false }] as never }),
    task('4', 'Paperwork', '2026-11-09', '2026-11-13'),
  ],
} as unknown as ProjectPlan;

const bar = (id: string, start: string, end: string, critical = false): Bar => ({ id, title: id, start, end, progress: 0, done: false, late: false, critical });

describe('lanes', () => {
  it('puts the critical path on the top row, above bars that start earlier', () => {
    const rows = lanes([bar('early', '2026-11-01', '2026-11-10'), bar('c1', '2026-11-05', '2026-11-12', true), bar('c2', '2026-11-13', '2026-11-20', true)]);
    expect(rows.map((r) => r.map((b) => b.id))).toEqual([['c1', 'c2'], ['early']]);
  });

  it('adds no empty row when nothing is critical', () => {
    expect(lanes([bar('a', '2026-11-01', '2026-11-10'), bar('b', '2026-11-11', '2026-11-12')]).map((r) => r.map((b) => b.id))).toEqual([['a', 'b']]);
  });
});

describe('PlanTimeline', () => {
  const tops = () => Array.from(container.querySelectorAll<HTMLElement>('[data-plan-view="timeline"] button')).map((b) => [b.textContent, b.style.top]);

  it('draws the critical tasks on the first row and marks them only while Critical path is on', async () => {
    await mount(<PlanTimeline plan={plan} onSelectTask={() => {}} showCritical />);
    const first = tops().filter(([, top]) => top === '24px').map(([name]) => name);
    expect(first).toEqual(['Order steel', 'Weld']);
    expect(container.querySelectorAll('[data-critical]')).toHaveLength(2);
    act(() => root.unmount());
    container.remove();
    await mount(<PlanTimeline plan={plan} onSelectTask={() => {}} showCritical={false} />);
    expect(tops().filter(([, top]) => top === '24px').map(([name]) => name)).toEqual(['Order steel', 'Weld']);
    expect(container.querySelectorAll('[data-critical]')).toHaveLength(0);
  });
});

describe('PlanNetwork and PlanUsage', () => {
  it('mark the critical path only while Critical path is on', async () => {
    const border = () => container.querySelector<HTMLElement>('[data-network-node="2"]')!.style.border;
    await mount(<PlanNetwork plan={plan} selectedTaskId="" onSelectTask={() => {}} showCritical />);
    expect(border()).toContain('2px');
    expect(container.querySelector('path[marker-end="url(#network-arrow-critical)"]')).not.toBeNull();
    act(() => root.unmount());
    container.remove();
    await mount(<PlanNetwork plan={plan} selectedTaskId="" onSelectTask={() => {}} showCritical={false} />);
    expect(border()).toContain('1px');
    expect(container.querySelector('path[marker-end="url(#network-arrow-critical)"]')).toBeNull();
    act(() => root.unmount());
    container.remove();
    await mount(<PlanUsage plan={plan} showCritical />);
    expect(container.querySelectorAll('td[data-critical]')).toHaveLength(2);
    act(() => root.unmount());
    container.remove();
    await mount(<PlanUsage plan={plan} showCritical={false} />);
    expect(container.querySelectorAll('td[data-critical]')).toHaveLength(0);
  });
});

describe('calendarRange', () => {
  it('shows a week, four weeks, a month or a quarter around the day', () => {
    const days = calendarRange('days', '2026-11-12');
    expect(days.heading).toBe('9 Nov – 15 Nov 2026');
    expect(days.blocks[0].days).toHaveLength(7);
    expect(days.step(1)).toBe('2026-11-16');
    const weeks = calendarRange('weeks', '2026-11-12');
    expect(weeks.heading).toBe('9 Nov – 6 Dec 2026');
    expect(weeks.blocks[0].days).toHaveLength(28);
    const month = calendarRange('months', '2026-11-12');
    expect(month.heading).toBe('November 2026');
    expect(month.blocks[0].days[0]).toBe('2026-10-26');
    expect(month.step(-1)).toBe('2026-10-01');
    const quarter = calendarRange('quarters', '2026-11-12');
    expect(quarter.heading).toBe('Q4 2026');
    expect(quarter.blocks.map((b) => b.title)).toEqual(['October', 'November', 'December']);
    expect(quarter.step(1)).toBe('2027-01-01');
  });
});

describe('PlanMonth', () => {
  const cells = () => container.querySelectorAll('[role="gridcell"]').length;
  const tasksOn = (day: string) => Array.from(container.querySelectorAll(`[data-day="${day}"] button`)).map((b) => b.textContent);

  it('follows the time scale', async () => {
    await mount(<PlanMonth plan={plan} selectedTaskId="" onSelectTask={() => {}} zoom="days" />);
    expect(cells()).toBe(7);
    expect(container.textContent).toContain('9 Nov – 15 Nov 2026');
    act(() => root.unmount());
    container.remove();
    await mount(<PlanMonth plan={plan} selectedTaskId="" onSelectTask={() => {}} zoom="quarters" />);
    expect(container.querySelectorAll('[role="grid"]')).toHaveLength(3);
    expect(container.textContent).toContain('Q4 2026');
    await click(container.querySelector('button[aria-label="Next quarter"]'));
    expect(container.textContent).toContain('Q1 2027');
  });

  it('lists the critical tasks first and marks them only while Critical path is on', async () => {
    await mount(<PlanMonth plan={plan} selectedTaskId="" onSelectTask={() => {}} zoom="days" showCritical />);
    expect(tasksOn('2026-11-10')).toEqual(['Order steel', 'Paint', 'Paperwork']);
    expect(container.querySelector('[data-day="2026-11-10"] [data-critical]')?.textContent).toBe('Order steel');
    act(() => root.unmount());
    container.remove();
    await mount(<PlanMonth plan={plan} selectedTaskId="" onSelectTask={() => {}} zoom="days" showCritical={false} />);
    expect(tasksOn('2026-11-10')).toEqual(['Paint', 'Order steel', 'Paperwork']);
    expect(container.querySelector('[data-critical]')).toBeNull();
  });
});

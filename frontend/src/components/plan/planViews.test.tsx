import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { PlanTask, ProjectPlan } from '../../api/client';
import { lanes, PlanTimeline, type Bar } from './PlanLines';
import { externalLinksText } from './GanttRows';
import { calendarRange, PlanMonth } from './PlanMonth';
import { PlanNetwork } from './PlanNetwork';
import { buildScale, buildTicks, isoWeek } from './ganttScale';
import { PlanUsage, usageColumns } from './PlanUsage';

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
  localStorage.clear();
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
    external_links: [],
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

  it('keeps a parent on a row above its sub-tasks, a level at a time', () => {
    const rows = lanes([
      { ...bar('weld', '2026-11-01', '2026-11-05', true), depth: 1 },
      { ...bar('paint', '2026-11-03', '2026-11-10'), depth: 1 },
      { ...bar('build', '2026-11-01', '2026-11-10', true), depth: 0 },
      { ...bar('ship', '2026-11-11', '2026-11-12'), depth: 0 },
    ]);
    expect(rows.map((r) => r.map((b) => b.id))).toEqual([['build'], ['ship'], ['weld'], ['paint']]);
  });

  it('adds no empty row when nothing is critical', () => {
    expect(lanes([bar('a', '2026-11-01', '2026-11-10'), bar('b', '2026-11-11', '2026-11-12')]).map((r) => r.map((b) => b.id))).toEqual([['a', 'b']]);
  });
});

describe('a task linked to another project', () => {
  const link = (direction: 'driven_by' | 'drives', title: string, project: string, conflict = false) => ({ direction, title, project_name: project, conflict }) as never;

  it('says which tasks it waits on and drives, and when the dates break a link', () => {
    expect(externalLinksText({ external_links: [link('driven_by', 'Order steel', 'Supply')] })).toBe('Waits on Order steel (Supply), in another project');
    expect(externalLinksText({ external_links: [link('driven_by', 'Order steel', 'Supply'), link('drives', 'Fit out', 'Site', true)] })).toBe(
      'Waits on Order steel (Supply); drives Fit out (Site), in other projects. The dates break a link',
    );
    expect(externalLinksText({ external_links: [link('drives', 'Fit out', 'Site')] })).toBe('Drives Fit out (Site), in another project');
  });

  it('is marked on the Timeline with the others drawn as they were', async () => {
    const linked = { ...plan, tasks: plan.tasks.map((t) => (t.title === 'Paint' ? { ...t, external_links: [link('driven_by', 'Order steel', 'Supply')] } : t)) } as unknown as ProjectPlan;
    await mount(<PlanTimeline plan={linked} onSelectTask={() => {}} />);
    const names = Array.from(container.querySelectorAll('[data-plan-view="timeline"] button')).map((b) => b.textContent);
    expect(names).toContain('⇄ Paint');
    expect(names).toContain('Weld');
    expect(container.querySelector('button[aria-label*="linked to another project"]')?.textContent).toBe('⇄ Paint');
  });
});

describe('PlanTimeline', () => {
  const tops = () => Array.from(container.querySelectorAll<HTMLElement>('[data-plan-view="timeline"] button')).map((b) => [b.textContent, b.style.top]);

  it('opens on the top-level tasks and digs down a level, the parent kept above its sub-tasks', async () => {
    const nested = {
      ...plan,
      tasks: [
        task('1', 'Build', '2026-11-02', '2026-11-20', { subtasks_total: 2 } as never),
        task('1.1', 'Cut', '2026-11-02', '2026-11-06', { depth: 1 }),
        task('1.2', 'Join', '2026-11-09', '2026-11-20', { depth: 1 }),
        task('2', 'Ship', '2026-11-23', '2026-11-27'),
      ],
    } as unknown as ProjectPlan;
    await mount(<PlanTimeline plan={nested} onSelectTask={() => {}} />);
    const level = container.querySelector<HTMLSelectElement>('select[aria-label="Level of detail"]')!;
    expect(level.value).toBe('1');
    expect(tops().map(([name]) => name)).toEqual(['Build', 'Ship']);
    expect(container.querySelectorAll('[data-summary]')).toHaveLength(0);
    await act(async () => {
      level.value = '2';
      level.dispatchEvent(new Event('change', { bubbles: true }));
    });
    // Build and Ship share the top row; Cut and Join the one beneath.
    expect(tops()).toEqual([
      ['Build', '24px'],
      ['Ship', '24px'],
      ['Cut', '48px'],
      ['Join', '48px'],
    ]);
    expect(container.querySelector('[data-summary]')?.textContent).toBe('Build');
    // The level is the one All projects opens on too.
    expect(localStorage.getItem('openv-plan-master-levels')).toBe('2');
  });

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

describe('the time scale in the other views', () => {
  it('buckets Resource usage by day, week, month or quarter', async () => {
    expect(usageColumns('weeks', '2026-11-02', '2026-12-20')).toHaveLength(7);
    expect(usageColumns('months', '2026-11-02', '2026-12-20')).toEqual([
      { start: '2026-11-01', end: '2026-11-30' },
      { start: '2026-12-01', end: '2026-12-31' },
    ]);
    expect(usageColumns('quarters', '2026-11-02', '2026-12-20')).toHaveLength(1);
    await mount(<PlanUsage plan={plan} zoom="months" />);
    const heads = Array.from(container.querySelectorAll('thead th')).map((th) => th.textContent);
    expect(heads).toEqual(['Who · days asked each month', 'Nov 2026', 'Dec 2026']);
    // Order steel: ten working days, all in November.
    expect(Array.from(container.querySelectorAll('tbody td')).map((td) => td.textContent)).toEqual(['10', '']);
  });

  it('draws the Timeline at the scale, so a long plan scrolls instead of squeezing', async () => {
    const paint = () => Array.from(container.querySelectorAll<HTMLElement>('[data-plan-view="timeline"] button')).find((b) => b.textContent === 'Paint')!;
    // A bar's width is its days at the scale's pixels a day: Paint runs 12 days.
    await mount(<PlanTimeline plan={plan} onSelectTask={() => {}} zoom="quarters" />);
    expect(parseFloat(paint().style.width)).toBeCloseTo(19.2);
    act(() => root.unmount());
    container.remove();
    await mount(<PlanTimeline plan={plan} onSelectTask={() => {}} zoom="days" />);
    expect(paint().style.width).toBe('360px');
    const view = container.querySelector<HTMLElement>('[data-plan-view="timeline"]')!;
    expect(view.style.overflow).toBe('auto');
    expect(parseFloat(container.querySelector<HTMLElement>('[data-plan-ticks]')!.style.width)).toBeGreaterThan(window.innerWidth);
  });
});

describe('week numbers', () => {
  it('counts ISO weeks: Monday to Sunday, week 1 holding the first Thursday', () => {
    expect(isoWeek('2026-11-09')).toBe(46);
    expect(isoWeek('2026-11-15')).toBe(46);
    expect(isoWeek('2026-11-16')).toBe(47);
    // 1 January 2027 is a Friday: still the last week of 2026, which has 53.
    expect(isoWeek('2027-01-01')).toBe(53);
    expect(isoWeek('2027-01-04')).toBe(1);
    // 30 December 2024 is the Monday of week 1 of 2025.
    expect(isoWeek('2024-12-30')).toBe(1);
  });

  it('labels each week of the Gantt at the weeks scale with its number, and no other scale', () => {
    const weeks = buildTicks(buildScale('weeks', '2026-11-02', '2026-12-20', '2026-11-12')).lower;
    expect(weeks.map((t) => t.label)).toEqual(expect.arrayContaining(['9 (W46)', '16 (W47)', '28 (W53)']));
    expect(weeks.every((t) => /^\d+ \(W\d+\)$/.test(t.label))).toBe(true);
    expect(buildTicks(buildScale('days', '2026-11-02', '2026-11-20', '2026-11-12')).lower.some((t) => t.label.includes('W'))).toBe(false);
  });

  it('gives each Monday of the Calendar its week number in the week views only', async () => {
    const weekOf = (day: string) => container.querySelector(`[data-day="${day}"] [data-week]`)?.textContent;
    await mount(<PlanMonth plan={plan} selectedTaskId="" onSelectTask={() => {}} zoom="weeks" />);
    expect(weekOf('2026-11-09')).toBe(' (W46)');
    expect(weekOf('2026-11-16')).toBe(' (W47)');
    expect(weekOf('2026-11-10')).toBeUndefined();
    expect(container.querySelectorAll('[data-week]')).toHaveLength(4);
    act(() => root.unmount());
    container.remove();
    await mount(<PlanMonth plan={plan} selectedTaskId="" onSelectTask={() => {}} zoom="months" />);
    expect(container.querySelector('[data-week]')).toBeNull();
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

import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import type { PlanMilestone, PlanTask, ProjectPlan } from '../../api/client';
import { DependencyLines } from './GanttLinks';
import { MilestoneLinks } from './MilestoneLinks';
import { buildScale } from './ganttScale';

// A milestone that waits for tasks (milestone-links): its panel lists them,
// adds and removes them and sets whether its date is fixed or by the plan,
// and the chart draws a line from each to the milestone's date.

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
  act(() => root.unmount());
  container.remove();
});

const task = (id: string, title: string, end: string, extra: Partial<PlanTask> = {}): PlanTask =>
  ({ id, title, wbs: id, depth: 0, span_start: '2027-03-01', span_end: end, subtasks_total: 0, relationships: [], done: false, float_until: null, ...extra }) as unknown as PlanTask;

const tasks = [task('1', 'Weld', '2027-03-05'), task('2', 'Paint', '2027-03-12'), task('3', 'Phase', '2027-03-12', { subtasks_total: 2 }), task('4', 'Ship', '2027-03-20')];
const plan = { today: '2027-03-02', tasks } as unknown as ProjectPlan;
const milestone = (extra: Partial<PlanMilestone> = {}): PlanMilestone =>
  ({ key: 'm1', kind: 'custom', id: 'm1', name: 'Frame done', date: '2027-03-10', fixed_date: '2027-03-10', date_mode: 'fixed', waits_for: ['1', '2'], conflict: true, ...extra }) as unknown as PlanMilestone;

const change = (el: HTMLSelectElement, value: string) =>
  act(async () => {
    Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('change', { bubbles: true }));
  });

describe('MilestoneLinks', () => {
  it('lists the tasks waited for, says which finishes after a fixed date, and removes one', async () => {
    const onLink = vi.fn();
    await mount(<MilestoneLinks plan={plan} milestone={milestone()} editable busy={false} onLink={onLink} onSelectTask={() => {}} />);
    expect(container.querySelector('h3')?.textContent).toBe('Waits for · 2');
    expect(container.textContent).toContain('A task it waits for finishes after 10 Mar');
    const rows = Array.from(container.querySelectorAll('[data-milestone-waits]')).map((r) => r.textContent);
    expect(rows[0]).toContain('1 Weldends 5 Mar');
    expect(rows[1]).toContain('2 Paintends 12 Mar');
    await act(async () => {
      container.querySelector('button[aria-label="Stop waiting for Paint"]')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(onLink).toHaveBeenCalledWith(expect.objectContaining({ key: 'm1' }), { waits_for: ['1'] });
    // A task with sub-tasks, and one already waited for, are not offered.
    const add = container.querySelector('#gantt-milestone-waits') as HTMLSelectElement;
    expect(Array.from(add.options).map((o) => o.value)).toEqual(['', '4']);
  });

  it('sets the date by the plan, and says where it then falls', async () => {
    const onLink = vi.fn();
    await mount(<MilestoneLinks plan={plan} milestone={milestone()} editable busy={false} onLink={onLink} onSelectTask={() => {}} />);
    await change(container.querySelector('select[aria-label="How the milestone\'s date is set"]')!, 'plan');
    expect(onLink).toHaveBeenCalledWith(expect.objectContaining({ key: 'm1' }), { date_mode: 'plan' });
    act(() => root.unmount());
    container.remove();
    await mount(<MilestoneLinks plan={plan} milestone={milestone({ date_mode: 'plan', date: '2027-03-12', conflict: false })} editable busy={false} onLink={onLink} onSelectTask={() => {}} />);
    expect(container.textContent).toContain('It falls on 12 Mar, and moves when the tasks below do.');
    expect(container.textContent).not.toContain('finishes after');
  });

  it('offers a release milestone its tasks but not its date', async () => {
    await mount(<MilestoneLinks plan={plan} milestone={milestone({ kind: 'release', conflict: false })} editable busy={false} onLink={() => {}} onSelectTask={() => {}} />);
    expect(container.querySelector('select[aria-label="How the milestone\'s date is set"]')).toBeNull();
    expect(container.querySelector('#gantt-milestone-waits')).not.toBeNull();
  });
});

describe('DependencyLines', () => {
  it('draws a line from each task a milestone waits for to its date, dashed when the task finishes after it', async () => {
    const scale = buildScale('days', '2027-03-01', '2027-03-20', '2027-03-02');
    await mount(<DependencyLines tasks={tasks} rows={tasks.map((t) => ({ id: t.id }))} folded={new Set()} scale={scale} milestones={[milestone()]} />);
    const weld = container.querySelector('path[data-link="1>milestone:m1"]')!;
    const paint = container.querySelector('path[data-link="2>milestone:m1"]')!;
    expect(weld.getAttribute('stroke-dasharray')).toBeNull();
    expect(paint.getAttribute('stroke-dasharray')).toBe('4 3');
    // Weld's line runs right to the milestone's day; Paint's runs back to it.
    const at = scale.x('2027-03-10') + scale.dayWidth / 2;
    expect(weld.getAttribute('d')).toBe(`M${scale.x('2027-03-05') + scale.dayWidth} 17 H${at - 1}`);
    expect(paint.getAttribute('d')).toBe(`M${scale.x('2027-03-12') + scale.dayWidth} 51 H${at + 1}`);
  });
});

import type { PlanMilestone, PlanTask } from '../../api/client';
import { FIT_MARGIN, buildScale, buildTicks, fitZoom, fromDay, isoWeekday, openingScroll, shortDate, spanLabel, toDay, todayScroll, unitSpan } from './ganttScale';
import { barTone, dependents, descendants, lagText, linkBetween, phoneGroups, shownAs, visibleTasks, workEnd, workSpan, workStart, worksOn } from './planModel';

// The plan's pure parts: the time scale the chart draws on, and what the
// views derive from the plan (folded rows, loops to refuse, the phone's
// groups).

const task = (id: string, extra: Partial<PlanTask> = {}): PlanTask => ({
  id,
  title: id,
  column: 'todo',
  assignee_type: 'user',
  assignee_id: null,
  parent_id: null,
  depth: 0,
  wbs: '1',
  start: null,
  end: null,
  span_start: null,
  span_end: null,
  duration_days: null,
  effort_days: null,
  float_days: null,
  critical: false,
  critical_subtasks: false,
  float_until: null,
  elapsed: false,
  deadline: null,
  deadline_missed: false,
  mode: 'manual',
  mode_own: '',
  constraint_type: 'asap',
  constraint_date: null,
  constraint_violated: false,
  early_start: null,
  early_finish: null,
  late_start: null,
  late_finish: null,
  free_float_days: null,
  assignments: [],
  fixed_cost: 0,
  cost: 0,
  effort_driven: false,
  overallocated: false,
  progress: 0,
  done: false,
  late: false,
  todos_done: 0,
  todos_total: 0,
  subtasks_done: 0,
  subtasks_total: 0,
  milestone: null,
  milestone_inherited: false,
  todos: [],
  depends_on: [],
  relationships: [],
  drives: [],
  conflicts: [],
  ...extra,
});

const milestone = (key: string, date: string | null): PlanMilestone => ({
  key,
  kind: 'custom',
  id: key,
  tasks: [],
  release_id: null,
  name: key,
  description: '',
  date,
  default_criteria: false,
  criteria: [],
  progress: 0,
  met: false,
  late: false,
});

describe('the Gantt scale', () => {
  it('reads a day as UTC, so a date never slips', () => {
    expect(fromDay(toDay('2027-03-01'))).toBe('2027-03-01');
    expect(toDay('2027-03-02') - toDay('2027-02-28')).toBe(2);
    expect(toDay('2028-03-01') - toDay('2028-02-28')).toBe(2); // a leap year
  });

  it('starts on a Monday and places a day by the zoom', () => {
    const scale = buildScale('weeks', '2027-03-03', '2027-03-20', '2027-03-10');
    expect(new Date(`${fromDay(scale.first)}T00:00:00Z`).getUTCDay()).toBe(1);
    expect(scale.x('2027-03-04') - scale.x('2027-03-03')).toBe(12);
    expect(scale.dayAt(scale.x('2027-03-15') + 5)).toBe('2027-03-15');
    expect(scale.width).toBe((scale.last - scale.first + 1) * 12);
  });

  it('reaches past the plan so the timeline scrolls, further forward than back', () => {
    const scale = buildScale('weeks', '2027-03-01', '2027-03-20', '2027-03-10', 1200);
    expect(scale.width).toBeGreaterThan(1200 * 1.5);
    const before = scale.x('2027-03-01');
    const after = scale.width - scale.x('2027-03-20');
    expect(before).toBeGreaterThanOrEqual(600);
    expect(after).toBeGreaterThan(before);
    // An empty plan is drawn around today.
    const empty = buildScale('days', null, null, '2027-03-10', 900);
    expect(empty.x('2027-03-10')).toBeGreaterThan(0);
    expect(empty.x('2027-03-10')).toBeLessThan(empty.width);
  });

  it('opens on the whole plan when it fits, else with today a third of the way in', () => {
    const fits = buildScale('weeks', '2027-03-01', '2027-03-20', '2027-03-10', 1200);
    expect(openingScroll(fits, '2027-03-01', '2027-03-20', '2027-03-10', 1200)).toBe(fits.x('2027-03-01') - FIT_MARGIN);
    const long = buildScale('weeks', '2026-01-01', '2028-01-01', '2027-03-10', 1200);
    expect(openingScroll(long, '2026-01-01', '2028-01-01', '2027-03-10', 1200)).toBe(Math.round(long.x('2027-03-10') - 400));
    expect(todayScroll(long, '2027-03-10', 900)).toBe(Math.round(long.x('2027-03-10') - 300));
  });

  it('heads the days with their months and marks weekends', () => {
    const scale = buildScale('days', '2027-03-01', '2027-03-07', '2027-03-03');
    const { upper, lower } = buildTicks(scale);
    expect(upper.map((t) => t.label)).toContain('Mar 2027');
    const saturday = lower.find((t) => t.left === scale.x('2027-03-06'));
    expect(saturday).toMatchObject({ label: '6', weekend: true, width: 30 });
    expect(lower.find((t) => t.left === scale.x('2027-03-03'))?.weekend).toBeUndefined();
  });

  it('shades the days the plan does not work, and schedules a click as one period of the zoom', () => {
    const scale = buildScale('days', '2027-03-01', '2027-03-07', '2027-03-03');
    const off = (days: number[]) => buildTicks(scale, days).lower.filter((t) => t.weekend && t.left >= scale.x('2027-03-01') && t.left <= scale.x('2027-03-07')).length;
    expect(off([1, 2, 3, 4, 5])).toBe(2);
    expect(off([1, 2, 3, 4, 5, 6])).toBe(1);
    expect(off([1, 2, 3, 4, 5, 6, 7])).toBe(0);
    // A holiday is shaded like a day off.
    expect(buildTicks(scale, [1, 2, 3, 4, 5, 6, 7], ['2027-03-03']).lower.filter((t) => t.weekend).length).toBe(1);
    expect(unitSpan('days', '2027-03-10')).toEqual({ start: '2027-03-10', end: '2027-03-10' });
    expect(unitSpan('weeks', '2027-03-10')).toEqual({ start: '2027-03-08', end: '2027-03-14' });
    expect(unitSpan('months', '2027-02-10')).toEqual({ start: '2027-02-01', end: '2027-02-28' });
    expect(unitSpan('quarters', '2027-05-10')).toEqual({ start: '2027-04-01', end: '2027-06-30' });
  });

  it('heads months with quarters and quarters with years, without a gap', () => {
    const months = buildTicks(buildScale('months', '2027-01-10', '2027-08-01', '2027-03-03'));
    expect(months.upper.map((t) => t.label)).toEqual(expect.arrayContaining(['Q1 2027', 'Q2 2027', 'Q3 2027']));
    expect(months.lower.map((t) => t.label)).toEqual(expect.arrayContaining(['Jan', 'Feb', 'Aug']));
    const scale = buildScale('quarters', '2026-11-01', '2028-02-01', '2027-03-03');
    const { upper, lower } = buildTicks(scale);
    expect(upper.map((t) => t.label)).toEqual(expect.arrayContaining(['2026', '2027', '2028']));
    expect(lower.reduce((sum, t) => sum + t.width, 0)).toBeCloseTo(scale.width, 5);
  });

  it('opens at the closest zoom at which the whole plan fits', () => {
    expect(fitZoom('2027-03-01', '2027-03-20', 1000)).toBe('days');
    expect(fitZoom('2027-03-01', '2027-04-10', 1000)).toBe('weeks');
    expect(fitZoom('2027-01-01', '2027-06-01', 1000)).toBe('months');
    expect(fitZoom('2027-01-01', '2028-06-01', 1000)).toBe('quarters');
    expect(fitZoom('2020-01-01', '2032-01-01', 1000)).toBe('quarters');
    expect(fitZoom(null, null, 1000)).toBe('weeks');
  });

  it('writes dates short, with the year when it is not this one', () => {
    expect(shortDate('2027-03-01', '2027-06-01')).toBe('1 Mar');
    expect(shortDate('2028-03-01', '2027-06-01')).toBe('1 Mar 2028');
    expect(spanLabel('2027-03-01', '2027-03-20', '2027-06-01')).toBe('1 Mar – 20 Mar');
    expect(spanLabel('2027-03-01', '2027-03-01', '2027-06-01')).toBe('1 Mar');
    expect(spanLabel(null, null)).toBe('Not scheduled');
  });
});

describe('the plan model', () => {
  const tasks = [
    task('a', { subtasks_total: 2 }),
    task('a1', { parent_id: 'a', depth: 1, subtasks_total: 1 }),
    task('a1x', { parent_id: 'a1', depth: 2 }),
    task('a2', { parent_id: 'a', depth: 1, depends_on: ['a1'] }),
    task('b', { depends_on: ['a2'] }),
  ];

  it('hides everything under a folded task', () => {
    expect(visibleTasks(tasks, new Set(['a'])).map((t) => t.id)).toEqual(['a', 'b']);
    expect(visibleTasks(tasks, new Set(['a1'])).map((t) => t.id)).toEqual(['a', 'a1', 'a2', 'b']);
    expect(visibleTasks(tasks, new Set())).toHaveLength(5);
  });

  it('finds what a task may not be put under, or depend on', () => {
    expect([...descendants(tasks, 'a')].sort()).toEqual(['a1', 'a1x', 'a2']);
    // a1 may not depend on itself, on a2 (which waits for it) or on b (which waits for a2).
    expect([...dependents(tasks, 'a1')].sort()).toEqual(['a1', 'a2', 'b']);
    expect([...dependents(tasks, 'b')]).toEqual(['b']);
  });

  it('stands a folded task in for the sub-tasks under it, at any depth', () => {
    expect(Object.fromEntries(shownAs(tasks, new Set(['a'])))).toEqual({ a: 'a', a1: 'a', a1x: 'a', a2: 'a', b: 'b' });
    expect(Object.fromEntries(shownAs(tasks, new Set(['a1'])))).toEqual({ a: 'a', a1: 'a1', a1x: 'a1', a2: 'a2', b: 'b' });
  });

  it('reads a link drawn between two bar ends as a relationship', () => {
    expect(linkBetween('end', 'start')).toEqual({ direction: 'drives', type: 'FS' });
    expect(linkBetween('start', 'start')).toEqual({ direction: 'drives', type: 'SS' });
    expect(linkBetween('end', 'end')).toEqual({ direction: 'drives', type: 'FF' });
    // From a start to a finish is start to finish.
    expect(linkBetween('start', 'end')).toEqual({ direction: 'drives', type: 'SF' });
  });

  it('counts a duration in working days', () => {
    const week = [1, 2, 3, 4, 5];
    // 1 March 2027 is a Monday.
    expect(isoWeekday('2027-03-01')).toBe(1);
    expect(isoWeekday('2027-03-07')).toBe(7);
    expect(worksOn(week, '2027-03-06')).toBe(false);
    expect(workSpan(week, '2027-03-01', '2027-03-12')).toBe(10);
    expect(workSpan(week, '2027-03-06', '2027-03-07')).toBe(1);
    expect(workEnd(week, '2027-03-01', 5)).toBe('2027-03-05');
    expect(workEnd(week, '2027-03-04', 3)).toBe('2027-03-08');
    // A start on a day off counts from the next working day.
    expect(workEnd(week, '2027-03-06', 1)).toBe('2027-03-08');
    expect(workStart(week, '2027-03-08', 3)).toBe('2027-03-04');
    expect(workStart(week, '2027-03-07', 2)).toBe('2027-03-04');
    expect(workEnd([1, 2, 3, 4, 5, 6, 7], '2027-03-04', 3)).toBe('2027-03-06');
    // A holiday is a day off whatever its weekday.
    const holidays = ['2027-03-03'];
    expect(worksOn(week, '2027-03-03', holidays)).toBe(false);
    expect(workSpan(week, '2027-03-01', '2027-03-05', holidays)).toBe(4);
    expect(workEnd(week, '2027-03-02', 2, holidays)).toBe('2027-03-04');
    expect(workStart(week, '2027-03-04', 2, holidays)).toBe('2027-03-02');
    expect(lagText(5, 'd')).toBe('5 working days later');
    expect(lagText(-1, 'ed')).toBe('1 elapsed day earlier');
    expect(lagText(50, '%')).toBe("50% of the driving task's duration later");
    expect(lagText(0, 'd')).toBe('');
  });

  it('colours a bar by its state', () => {
    expect(barTone(task('t', { done: true, late: true }))).toBe('done');
    expect(barTone(task('t', { late: true }))).toBe('late');
    expect(barTone(task('t', { progress: 10 }))).toBe('active');
    expect(barTone(task('t', { column: 'review' }))).toBe('active');
    expect(barTone(task('t'))).toBe('idle');
  });

  it('groups the phone schedule under the milestone each task leads to', () => {
    const groups = phoneGroups(
      [
        task('early', { span_end: '2027-02-01' }),
        task('on-the-day', { span_end: '2027-03-01' }),
        task('sub', { depth: 1, parent_id: 'early', span_end: '2027-06-01' }),
        task('mid', { span_end: '2027-03-02' }),
        task('late', { span_end: '2027-09-01' }),
        task('loose'),
      ],
      [milestone('m1', '2027-03-01'), milestone('m2', '2027-06-01'), milestone('undated', null)],
    );
    expect(groups.map((g) => [g.key, g.tasks.map((t) => t.id)])).toEqual([
      ['m1', ['early', 'on-the-day']],
      ['m2', ['mid']],
      ['later', ['late']],
      ['unscheduled', ['loose']],
    ]);
    // A milestone with nothing due before it still shows; an empty tail does not.
    expect(phoneGroups([], [milestone('m1', '2027-03-01')]).map((g) => g.key)).toEqual(['m1']);
    expect(phoneGroups([task('x', { span_end: '2027-01-01' })], [])[0].title).toBe('Scheduled');
  });
});

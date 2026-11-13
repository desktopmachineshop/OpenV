// How the chart lists the plan's tasks: which it leaves out, the order
// they come in, the groups they are gathered under and the columns beside
// their names. The outline is the default: every task under its parent.
import type { PlanScheduleInput, PlanTask, ProjectPlan } from '../../api/client';
import { columnLabel, personName, visibleTasks } from './planModel';

export type PlanFilter = 'all' | 'incomplete' | 'late' | 'critical' | 'unscheduled' | 'behind' | 'overallocated';
export type PlanSort = 'outline' | 'start' | 'finish' | 'name' | 'float' | 'progress';
export type PlanGroup = 'none' | 'assignee' | 'milestone' | 'status';
export type PlanColumn = 'start' | 'end' | 'duration' | 'effort' | 'percent' | 'float' | 'cost' | 'who';

export interface PlanView {
  search: string;
  filter: PlanFilter;
  sort: PlanSort;
  group: PlanGroup;
  columns: PlanColumn[];
  /** Leave out the tasks with no dates: what has nothing to draw on the chart. */
  dated: boolean;
}

export const DEFAULT_VIEW: PlanView = { search: '', filter: 'all', sort: 'outline', group: 'none', columns: [], dated: false };

const DATED_KEY = 'openv-gantt-dated-only';

/** The view the chart opens with: the default, with "Dated only" as it was last left in this browser. */
export const initialView = (): PlanView => {
  try {
    return { ...DEFAULT_VIEW, dated: localStorage.getItem(DATED_KEY) === '1' };
  } catch {
    return DEFAULT_VIEW;
  }
};

/** Remembers "Dated only" for the next visit. */
export const rememberDated = (on: boolean): void => {
  try {
    if (on) localStorage.setItem(DATED_KEY, '1');
    else localStorage.removeItem(DATED_KEY);
  } catch {
    // It still applies for this visit.
  }
};

export const FILTERS: { value: PlanFilter; label: string }[] = [
  { value: 'all', label: 'All tasks' },
  { value: 'incomplete', label: 'Not done' },
  { value: 'late', label: 'Late' },
  { value: 'critical', label: 'Critical' },
  { value: 'behind', label: 'Behind the status date' },
  { value: 'unscheduled', label: 'Not scheduled' },
  { value: 'overallocated', label: 'Over-allocated' },
];

export const SORTS: { value: PlanSort; label: string }[] = [
  { value: 'outline', label: 'Outline' },
  { value: 'start', label: 'Start' },
  { value: 'finish', label: 'Finish' },
  { value: 'name', label: 'Name' },
  { value: 'float', label: 'Least float' },
  { value: 'progress', label: 'Least done' },
];

export const GROUPS: { value: PlanGroup; label: string }[] = [
  { value: 'none', label: 'No groups' },
  { value: 'assignee', label: 'Who' },
  { value: 'milestone', label: 'Milestone' },
  { value: 'status', label: 'Board column' },
];

/** A column beside the task names: its heading, width, and the field an edit writes. */
export interface ColumnSpec {
  key: PlanColumn;
  label: string;
  width: number;
  /** How the cell is edited; none when the plan works the value out. */
  edit?: 'date' | 'number';
}

export const COLUMNS: ColumnSpec[] = [
  { key: 'start', label: 'Start', width: 92, edit: 'date' },
  { key: 'end', label: 'Finish', width: 92, edit: 'date' },
  { key: 'duration', label: 'Days', width: 52, edit: 'number' },
  { key: 'effort', label: 'Effort', width: 56, edit: 'number' },
  { key: 'percent', label: '% done', width: 58, edit: 'number' },
  { key: 'float', label: 'Float', width: 50 },
  { key: 'cost', label: 'Cost', width: 84 },
  { key: 'who', label: 'Who', width: 120 },
];

/** The chosen columns, in the order the chooser lists them. */
export const shownColumns = (view: PlanView): ColumnSpec[] => COLUMNS.filter((c) => view.columns.includes(c.key));

/** How much wider the chosen columns make the task column. */
export const columnsWidth = (view: PlanView): number => shownColumns(view).reduce((w, c) => w + c.width, 0);

/** The list is the outline itself: tasks under their parents, in the plan's order. */
export const isOutline = (view: PlanView): boolean => view.sort === 'outline' && view.group === 'none';

/** A row of the chart: a task, or the heading of a group of them. */
export type PlanRow = { id: string; task: PlanTask; flat: boolean } | { id: string; task?: undefined; label: string; count: number };

const matches = (t: PlanTask, view: PlanView): boolean => {
  const q = view.search.trim().toLowerCase();
  if (q && !t.title.toLowerCase().includes(q) && !t.wbs.startsWith(q)) return false;
  if (view.dated && !t.span_start) return false;
  switch (view.filter) {
    case 'incomplete':
      return !t.done;
    case 'late':
      return t.late || t.deadline_missed;
    case 'critical':
      return t.critical || t.critical_subtasks;
    case 'behind':
      return t.behind_schedule;
    case 'unscheduled':
      return !t.span_start;
    case 'overallocated':
      return t.overallocated;
    default:
      return true;
  }
};

/** The names a task is filed under when grouped; a task with nobody, or no milestone, has one group of its own. */
const groupsOf = (plan: ProjectPlan, t: PlanTask, group: PlanGroup): string[] => {
  if (group === 'assignee') return t.assignments.length ? t.assignments.map((a) => personName(plan.people, a)) : ['Nobody'];
  if (group === 'milestone') return [plan.milestones.find((m) => m.key === t.milestone)?.name || 'No milestone'];
  return [columnLabel(t.column)];
};

const text = (s: string | null) => s || '9999-99-99';
const SORTERS: Record<Exclude<PlanSort, 'outline'>, (a: PlanTask, b: PlanTask) => number> = {
  start: (a, b) => text(a.span_start).localeCompare(text(b.span_start)),
  finish: (a, b) => text(a.span_end).localeCompare(text(b.span_end)),
  name: (a, b) => a.title.localeCompare(b.title),
  float: (a, b) => (a.float_days ?? 1e9) - (b.float_days ?? 1e9),
  progress: (a, b) => a.progress - b.progress,
};

/**
 * The rows the chart shows. As an outline, a task that matches keeps the
 * tasks it sits under, and a folded task hides its sub-tasks. Sorted or
 * grouped, the list is flat: the tasks with no sub-tasks, since a task with
 * sub-tasks only sums them.
 */
export const arrange = (plan: ProjectPlan, folded: ReadonlySet<string>, view: PlanView): PlanRow[] => {
  const filtering = view.search.trim() !== '' || view.filter !== 'all' || view.dated;
  if (isOutline(view)) {
    let tasks = plan.tasks;
    if (filtering) {
      const keep = new Set<string>();
      const byId = new Map(plan.tasks.map((t) => [t.id, t]));
      for (const t of plan.tasks) {
        if (!matches(t, view)) continue;
        for (let up: PlanTask | undefined = t; up && !keep.has(up.id); up = up.parent_id ? byId.get(up.parent_id) : undefined) keep.add(up.id);
      }
      tasks = tasks.filter((t) => keep.has(t.id));
    }
    return visibleTasks(tasks, folded).map((task) => ({ id: task.id, task, flat: false }));
  }
  const leaves = plan.tasks.filter((t) => t.subtasks_total === 0 && matches(t, view));
  if (view.sort !== 'outline') leaves.sort(SORTERS[view.sort]);
  if (view.group === 'none') return leaves.map((task) => ({ id: task.id, task, flat: true }));
  const groups = new Map<string, PlanTask[]>();
  for (const t of leaves) {
    for (const g of groupsOf(plan, t, view.group)) groups.set(g, [...(groups.get(g) || []), t]);
  }
  const rows: PlanRow[] = [];
  for (const label of [...groups.keys()].sort((a, b) => a.localeCompare(b))) {
    const list = groups.get(label)!;
    rows.push({ id: `group:${label}`, label, count: list.length });
    // A task on two people is listed under each: its second row has an id of its own.
    for (const task of list) rows.push({ id: rows.some((r) => r.id === task.id) ? `${label}:${task.id}` : task.id, task, flat: true });
  }
  return rows;
};

export type RowMove = 'up' | 'down' | 'indent' | 'outdent';

/** What moving a task one place up or down, or in or out a level, asks of its schedule; null when it cannot go that way. */
export const moveChange = (tasks: PlanTask[], id: string, move: RowMove): PlanScheduleInput | null => {
  const t = tasks.find((x) => x.id === id);
  if (!t) return null;
  const siblings = tasks.filter((x) => x.parent_id === t.parent_id);
  const at = siblings.findIndex((x) => x.id === id);
  switch (move) {
    case 'up':
      return at > 0 ? { after_id: at > 1 ? siblings[at - 2].id : '' } : null;
    case 'down':
      return at < siblings.length - 1 ? { after_id: siblings[at + 1].id } : null;
    case 'indent':
      return at > 0 ? { parent_id: siblings[at - 1].id } : null;
    default: {
      const parent = tasks.find((x) => x.id === t.parent_id);
      return parent ? { parent_id: parent.parent_id || '', after_id: parent.id } : null;
    }
  }
};

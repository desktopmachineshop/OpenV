// What the plan's views share beyond the API's answer: which rows a folded
// outline shows, a task's descendants, the colours of a bar and a milestone,
// and the phone's grouping of tasks under the milestone they lead to.
import type { PlanMilestone, PlanRelationshipDirection, PlanRelationshipType, PlanTask } from '../../api/client';
import { addDays, isoWeekday } from './ganttScale';
import type { ChipTone } from '../ui';
import { BOARD_COLUMNS } from '../kanban/columns';

/** The rows of the outline with the sub-tasks of folded tasks left out. */
export const visibleTasks = (tasks: PlanTask[], folded: ReadonlySet<string>): PlanTask[] => {
  const hidden = new Set<string>();
  return tasks.filter((t) => {
    if (t.parent_id && (hidden.has(t.parent_id) || folded.has(t.parent_id))) {
      hidden.add(t.id);
      return false;
    }
    return true;
  });
};

/** The ids of every task beneath one, at any depth. */
export const descendants = (tasks: PlanTask[], id: string): Set<string> => {
  const out = new Set<string>();
  // The outline lists a parent before its sub-tasks, so one pass reaches all.
  for (const t of tasks) {
    if (t.parent_id && (t.parent_id === id || out.has(t.parent_id))) out.add(t.id);
  }
  return out;
};

/** The relationship types, in the order a chooser shows them, as a Gantt names them. */
export const RELATIONSHIP_TYPES: { value: PlanRelationshipType; label: string; hint: string }[] = [
  { value: 'FS', label: 'Finish to start', hint: 'Starts after that task finishes' },
  { value: 'SS', label: 'Start to start', hint: 'Starts no earlier than that task starts' },
  { value: 'FF', label: 'Finish to finish', hint: 'Finishes no earlier than that task finishes' },
];

export const relationshipLabel = (type: PlanRelationshipType): string =>
  RELATIONSHIP_TYPES.find((r) => r.value === type)?.label ?? type;

/** What a broken relationship of each type means, for the chip's tooltip. */
export const RELATIONSHIP_BROKEN: Record<PlanRelationshipType, string> = {
  FS: 'This task starts before that one finishes',
  SS: 'This task starts before that one starts',
  FF: 'This task finishes before that one finishes',
};

/** The tasks that would make a loop if `id` were driven by them: itself and whatever it drives. */
export const dependents = (tasks: PlanTask[], id: string): Set<string> => {
  const out = new Set<string>([id]);
  for (let grew = true; grew; ) {
    grew = false;
    for (const t of tasks) {
      if (!out.has(t.id) && t.depends_on.some((d) => out.has(d))) {
        out.add(t.id);
        grew = true;
      }
    }
  }
  return out;
};

/** A relationship as the task that has it open reads it: its type, and which way it runs. */
export interface RelationshipView {
  type: PlanRelationshipType;
  direction: PlanRelationshipDirection;
}

/**
 * The six ways one task can relate to another, worded from the side of the
 * task being edited: "this task" is the subject of each. The first three are
 * driven by the other task, the last three drive it.
 */
export const RELATIONSHIP_VIEWS: (RelationshipView & { label: string })[] = [
  { direction: 'driven_by', type: 'FS', label: 'Starts after it finishes' },
  { direction: 'driven_by', type: 'SS', label: 'Starts no earlier than it starts' },
  { direction: 'driven_by', type: 'FF', label: 'Finishes no earlier than it finishes' },
  { direction: 'drives', type: 'FS', label: 'Finishes before it starts' },
  { direction: 'drives', type: 'SS', label: 'Starts no later than it starts' },
  { direction: 'drives', type: 'FF', label: 'Finishes no later than it finishes' },
];

export const viewKey = (v: RelationshipView): string => `${v.direction}:${v.type}`;

export const relationshipViewLabel = (v: RelationshipView): string =>
  RELATIONSHIP_VIEWS.find((r) => r.direction === v.direction && r.type === v.type)?.label ?? v.type;

/** An end of a task's bar: where a link drawn on the chart leaves or lands. */
export type BarEnd = 'start' | 'end';

/**
 * The relationship a link dragged from one end of a bar to an end of another
 * makes, from the first task's side. Finish to start, start to start and
 * finish to finish drive the task the link lands on; a link from a start to
 * a finish is finish to start the other way round.
 */
export const linkBetween = (from: BarEnd, to: BarEnd): RelationshipView => {
  if (from === 'end') return { direction: 'drives', type: to === 'start' ? 'FS' : 'FF' };
  return to === 'start' ? { direction: 'drives', type: 'SS' } : { direction: 'driven_by', type: 'FS' };
};

/** The row that stands for a task on the chart: itself, or the folded task it is under. */
export const shownAs = (tasks: PlanTask[], folded: ReadonlySet<string>): Map<string, string> => {
  const out = new Map<string, string>();
  // The outline lists a parent before its sub-tasks.
  for (const t of tasks) {
    const parent = t.parent_id ? out.get(t.parent_id) : undefined;
    out.set(t.id, parent !== undefined && (parent !== t.parent_id || folded.has(t.parent_id!)) ? parent : t.id);
  }
  return out;
};

/** Whether the plan works on a day. */
export const worksOn = (workingDays: number[], date: string): boolean => workingDays.includes(isoWeekday(date));

/** A task's duration from start to end in working days, counting both ends; at least 1. */
export const workSpan = (workingDays: number[], start: string, end: string): number => {
  let n = 0;
  for (let d = start; d <= end; d = addDays(d, 1)) if (worksOn(workingDays, d)) n++;
  return Math.max(n, 1);
};

const step = (workingDays: number[], from: string, duration: number, by: 1 | -1): string => {
  if (workingDays.length === 0) return from;
  let d = from;
  while (!worksOn(workingDays, d)) d = addDays(d, by);
  for (let left = duration - 1; left > 0; ) {
    d = addDays(d, by);
    if (worksOn(workingDays, d)) left--;
  }
  return d;
};

/** The due date of a task that starts on a day and runs for so many working days. */
export const workEnd = (workingDays: number[], start: string, duration: number): string => step(workingDays, start, duration, 1);

/** The start of a task that is due on a day and runs for so many working days. */
export const workStart = (workingDays: number[], due: string, duration: number): string => step(workingDays, due, duration, -1);

export const WEEKDAYS: { day: number; short: string; name: string }[] = [
  { day: 1, short: 'M', name: 'Monday' },
  { day: 2, short: 'T', name: 'Tuesday' },
  { day: 3, short: 'W', name: 'Wednesday' },
  { day: 4, short: 'T', name: 'Thursday' },
  { day: 5, short: 'F', name: 'Friday' },
  { day: 6, short: 'S', name: 'Saturday' },
  { day: 7, short: 'S', name: 'Sunday' },
];

export const columnLabel = (column: string): string => BOARD_COLUMNS.find((c) => c.key === column)?.label ?? column;

export type BarTone = 'done' | 'late' | 'active' | 'idle';

/** A task's state as its bar shows it. */
export const barTone = (t: Pick<PlanTask, 'done' | 'late' | 'progress' | 'column'>): BarTone => {
  if (t.done) return 'done';
  if (t.late) return 'late';
  return t.progress > 0 || t.column === 'in-progress' || t.column === 'review' ? 'active' : 'idle';
};

/** The theme tokens a bar is drawn in: its fill, its track and its outline. */
export const BAR_COLORS: Record<BarTone, { fill: string; track: string; border: string }> = {
  done: { fill: 'var(--success)', track: 'var(--tint-green)', border: 'var(--success)' },
  late: { fill: 'var(--danger)', track: 'var(--tint-red)', border: 'var(--danger)' },
  active: { fill: 'var(--accent)', track: 'var(--tint-blue)', border: 'var(--accent)' },
  idle: { fill: 'var(--accent)', track: 'var(--surface-alt)', border: 'var(--neutral-mid)' },
};

export const TASK_TONE: Record<BarTone, ChipTone> = { done: 'success', late: 'danger', active: 'info', idle: 'neutral' };

/** A milestone's colour: met, late, or its kind's (a release's is the agent purple's neighbour). */
export const milestoneColor = (m: Pick<PlanMilestone, 'met' | 'late' | 'kind'>): string => {
  if (m.met) return 'var(--success)';
  if (m.late) return 'var(--danger)';
  return m.kind === 'release' ? 'var(--purple)' : 'var(--accent-strong)';
};

export const milestoneState = (m: Pick<PlanMilestone, 'met' | 'late'>): { label: string; tone: ChipTone } => {
  if (m.met) return { label: 'Met', tone: 'success' };
  if (m.late) return { label: 'Late', tone: 'danger' };
  return { label: 'Open', tone: 'neutral' };
};

export interface PhoneGroup {
  key: string;
  /** The milestone the group's tasks are due by; null for the tail groups. */
  milestone: PlanMilestone | null;
  title: string;
  tasks: PlanTask[];
}

/**
 * The phone's schedule: the top-level tasks under the first dated milestone
 * they are due on or before, then those due after the last milestone, then
 * the unscheduled. A sub-task stays under its parent, which the row counts.
 */
export const phoneGroups = (tasks: PlanTask[], milestones: PlanMilestone[]): PhoneGroup[] => {
  const dated = milestones.filter((m) => m.date);
  const groups: PhoneGroup[] = dated.map((m) => ({ key: m.key, milestone: m, title: m.name, tasks: [] }));
  const later: PhoneGroup = { key: 'later', milestone: null, title: dated.length ? 'After the last milestone' : 'Scheduled', tasks: [] };
  const unscheduled: PhoneGroup = { key: 'unscheduled', milestone: null, title: 'Not scheduled', tasks: [] };
  for (const t of tasks) {
    if (t.depth > 0) continue;
    if (!t.span_end) {
      unscheduled.tasks.push(t);
      continue;
    }
    const i = dated.findIndex((m) => t.span_end! <= m.date!);
    (i >= 0 ? groups[i] : later).tasks.push(t);
  }
  return [...groups, later, unscheduled].filter((g) => g.milestone || g.tasks.length > 0);
};

/** The presets Add milestone offers: a name and the criteria that go with it. */
export const MILESTONE_PRESETS: { key: string; label: string; name: string; criteria: { metric: string; target: number }[] }[] = [
  { key: 'blank', label: 'No criteria yet', name: '', criteria: [] },
  { key: 'definition', label: 'Definition complete', name: 'Definition complete', criteria: [{ metric: 'needs_addressed', target: 100 }] },
  {
    key: 'requirements',
    label: 'Requirements approved',
    name: 'Requirements approved',
    criteria: [
      { metric: 'needs_addressed', target: 100 },
      { metric: 'requirements_approved', target: 100 },
    ],
  },
  { key: 'design', label: 'Design complete', name: 'Design complete', criteria: [{ metric: 'requirements_designed', target: 100 }] },
  {
    key: 'verification',
    label: 'Verification complete',
    name: 'Verification complete',
    criteria: [
      { metric: 'requirements_tested', target: 100 },
      { metric: 'requirements_verified', target: 100 },
    ],
  },
];

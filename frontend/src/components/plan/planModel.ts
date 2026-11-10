// What the plan's views share beyond the API's answer: which rows a folded
// outline shows, a task's descendants, the colours of a bar and a milestone,
// and the phone's grouping of tasks under the milestone they lead to.
import type { PlanMilestone, PlanTask } from '../../api/client';
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

/** The tasks that would make a loop if `id` depended on them: itself and whatever waits on it. */
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

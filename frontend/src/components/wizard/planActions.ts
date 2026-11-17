import { text } from './artifactTemplates';

/**
 * The plan as V&V Assistant cards: scheduling a board task (its dates,
 * duration, effort, progress, parent task, milestone and the tasks it
 * waits on), milestones and their criteria, to-dos on a task, ticking a
 * to-do off, and the plan-wide actions (recalculate, level, baseline,
 * reschedule what has not started).
 *
 * This is the pure half, as projectActions.ts is for the other cards: what
 * a card means, or why it means nothing yet, and what it says on its face.
 * Tasks and milestones are named as a person names them, by title or
 * outline number; planActionsApply.ts finds them when the card is applied.
 * The shapes are the ones the server's prompt teaches
 * (internal/api/guided_copilot_actions.go).
 */

export const PLAN_ACTION_LABELS: Record<string, string> = {
  plan_task: 'Plan task',
  milestone: 'Milestone',
  task_todo: 'Task to-do',
  todo_done: 'To-do',
  plan: 'Plan',
};
export const PLAN_ACTION_KINDS: ReadonlySet<string> = new Set(Object.keys(PLAN_ACTION_LABELS));

export const BOARD_COLUMN_KEYS = ['backlog', 'todo', 'in-progress', 'review', 'done'] as const;
export const PLAN_WIDE_ACTIONS = ['recalculate', 'level', 'baseline', 'reschedule_remaining'] as const;
const REL_TYPES = ['FS', 'SS', 'FF', 'SF'] as const;
const DATE = /^\d{4}-\d{2}-\d{2}$/;

/** What a card asks of a task's schedule, with tasks and the milestone still named in words. */
export interface ScheduleFields {
  start?: string;
  due?: string;
  duration_days?: number;
  effort_days?: number;
  percent_complete?: number;
  deadline?: string;
  /** The task it is a sub-task of; empty makes it a top-level task. */
  parent?: string;
  /** The milestone it counts toward; empty takes it off. */
  milestone?: string;
  /** The tasks that drive it; an empty list clears them. */
  after?: { task: string; type: (typeof REL_TYPES)[number]; lag_days: number }[];
}

export type PlanAction =
  | { kind: 'plan_task'; task: string; title?: string; column?: string; schedule: ScheduleFields }
  | { kind: 'milestone'; milestone?: string; fields: { name?: string; description?: string; date?: string; criteria?: { metric: string; target: number }[] } }
  | { kind: 'task_todo'; task: string; text: string; due?: string }
  | { kind: 'todo_done'; text: string; done: boolean }
  | { kind: 'plan'; action: (typeof PLAN_WIDE_ACTIONS)[number]; name?: string };

type Suggestion = { kind: string; [key: string]: any };
type Parsed<T> = { ok: T } | { reason: string };

const number = (value: unknown, name: string, max: number, whole: boolean): Parsed<number> => {
  const n = typeof value === 'number' ? value : Number(text(value));
  if (!Number.isFinite(n) || n < 0 || n > max || (whole && !Number.isInteger(n))) {
    return { reason: `"${text(value)}" is not a ${name}; give a ${whole ? 'whole ' : ''}number from 0 to ${max}.` };
  }
  return { ok: n };
};

/** The schedule a card's fields ask for, or why they cannot be read. A date may be empty, which clears it. */
export const scheduleFields = (s: Suggestion): Parsed<ScheduleFields> => {
  const out: ScheduleFields = {};
  for (const key of ['start', 'due', 'deadline'] as const) {
    if (s[key] === undefined) continue;
    const d = text(s[key]);
    if (d && !DATE.test(d)) return { reason: `"${d}" is not a date; write it as YYYY-MM-DD.` };
    out[key] = d;
  }
  const numbers: [keyof ScheduleFields & ('duration_days' | 'effort_days' | 'percent_complete'), string, number, boolean][] = [
    ['duration_days', 'duration in working days', 3650, true],
    ['effort_days', 'number of days of effort', 100000, false],
    ['percent_complete', 'percentage complete', 100, true],
  ];
  for (const [key, name, max, whole] of numbers) {
    if (s[key] === undefined) continue;
    const n = number(s[key], name, max, whole);
    if ('reason' in n) return n;
    out[key] = n.ok;
  }
  if (s.parent !== undefined) out.parent = text(s.parent);
  if (s.milestone !== undefined) out.milestone = text(s.milestone);
  if (s.after !== undefined) {
    if (!Array.isArray(s.after)) return { reason: 'The tasks it waits on must be a list.' };
    out.after = [];
    for (const raw of s.after) {
      const task = text(typeof raw === 'string' ? raw : raw?.task);
      const type = (text(typeof raw === 'string' ? '' : raw?.type) || 'FS').toUpperCase();
      const lag = typeof raw === 'string' || raw?.lag_days === undefined ? 0 : Number(raw.lag_days);
      if (!task) return { reason: 'A task it waits on has no name.' };
      if (!(REL_TYPES as readonly string[]).includes(type)) return { reason: `"${type}" is not a relationship; use FS, SS, FF or SF.` };
      if (!Number.isInteger(lag) || Math.abs(lag) > 3650) return { reason: `The offset after ${task} is not a whole number of working days.` };
      out.after.push({ task, type: type as (typeof REL_TYPES)[number], lag_days: lag });
    }
  }
  return { ok: out };
};

export type PlanActionPlan = { outcome: 'action'; action: PlanAction } | { outcome: 'refused'; reason: string };
const refuse = (reason: string): PlanActionPlan => ({ outcome: 'refused', reason });
const act = (action: PlanAction): PlanActionPlan => ({ outcome: 'action', action });

/** What a plan card means, or the reason a person can act on why not. */
export const planPlanAction = (s: Suggestion): PlanActionPlan => {
  switch (s.kind) {
    case 'plan_task': {
      const task = text(s.task);
      if (!task) return refuse('The card names no task.');
      const schedule = scheduleFields(s);
      if ('reason' in schedule) return refuse(schedule.reason);
      const title = s.title === undefined ? undefined : text(s.title);
      if (title === '') return refuse('A task cannot have an empty title.');
      const column = s.column === undefined ? undefined : text(s.column);
      if (column !== undefined && !(BOARD_COLUMN_KEYS as readonly string[]).includes(column)) {
        return refuse(`"${column}" is not a board column; use ${BOARD_COLUMN_KEYS.join(', ')}.`);
      }
      if (title === undefined && column === undefined && Object.keys(schedule.ok).length === 0) return refuse(`The change to ${task} changes nothing.`);
      return act({ kind: 'plan_task', task, ...(title ? { title } : {}), ...(column ? { column } : {}), schedule: schedule.ok });
    }

    case 'milestone': {
      const fields: { name?: string; description?: string; date?: string; criteria?: { metric: string; target: number }[] } = {};
      if (s.name !== undefined) fields.name = text(s.name);
      if (s.description !== undefined) fields.description = String(s.description ?? '');
      if (s.date !== undefined) {
        const d = text(s.date);
        if (d && !DATE.test(d)) return refuse(`"${d}" is not a date; write it as YYYY-MM-DD.`);
        fields.date = d;
      }
      if (s.criteria !== undefined) {
        if (!Array.isArray(s.criteria)) return refuse('A milestone’s criteria must be a list.');
        fields.criteria = [];
        for (const c of s.criteria) {
          const metric = text(c?.metric);
          const target = Number(c?.target ?? 100);
          if (!metric) return refuse('A criterion names no metric.');
          if (!Number.isInteger(target) || target < 1 || target > 100) return refuse(`The target for ${metric} is not a percentage from 1 to 100.`);
          fields.criteria.push({ metric, target });
        }
      }
      const existing = text(s.milestone);
      if (!existing && !fields.name) return refuse('A new milestone needs a name.');
      if (fields.name === '') return refuse('A milestone cannot have an empty name.');
      if (existing && Object.keys(fields).length === 0) return refuse(`The change to ${existing} changes nothing.`);
      return act({ kind: 'milestone', ...(existing ? { milestone: existing } : {}), fields });
    }

    case 'task_todo': {
      const task = text(s.task);
      const body = text(s.text);
      if (!task) return refuse('The to-do names no task.');
      if (!body) return refuse('The to-do says nothing to do.');
      const due = text(s.due);
      if (due && !DATE.test(due)) return refuse(`"${due}" is not a date; write it as YYYY-MM-DD.`);
      return act({ kind: 'task_todo', task, text: body, ...(due ? { due } : {}) });
    }

    case 'todo_done': {
      const body = text(s.text);
      if (!body) return refuse('The card names no to-do.');
      return act({ kind: 'todo_done', text: body, done: s.done !== false });
    }

    case 'plan': {
      const action = text(s.action);
      if (!(PLAN_WIDE_ACTIONS as readonly string[]).includes(action)) {
        return refuse(`"${action}" is not something a plan card does; use ${PLAN_WIDE_ACTIONS.join(', ')}.`);
      }
      const name = text(s.name);
      return act({ kind: 'plan', action: action as (typeof PLAN_WIDE_ACTIONS)[number], ...(name ? { name } : {}) });
    }

    default:
      return refuse(`I do not know how to apply a "${s.kind}".`);
  }
};

/** A schedule in a few words, for a card's second line. */
export const scheduleSummary = (f: ScheduleFields): string =>
  [
    f.start !== undefined ? (f.start ? `starts ${f.start}` : 'no start date') : '',
    f.due !== undefined ? (f.due ? `due ${f.due}` : 'no due date') : '',
    f.duration_days !== undefined ? `${f.duration_days} working days` : '',
    f.effort_days !== undefined ? `${f.effort_days} days of effort` : '',
    f.percent_complete !== undefined ? `${f.percent_complete}% complete` : '',
    f.deadline !== undefined ? (f.deadline ? `deadline ${f.deadline}` : 'no deadline') : '',
    f.parent !== undefined ? (f.parent ? `under ${f.parent}` : 'a top-level task') : '',
    f.milestone !== undefined ? (f.milestone ? `for ${f.milestone}` : 'off its milestone') : '',
    f.after !== undefined ? (f.after.length ? `after ${f.after.map((a) => `${a.task}${a.type === 'FS' ? '' : ` (${a.type})`}${a.lag_days ? ` ${a.lag_days > 0 ? '+' : ''}${a.lag_days}d` : ''}`).join(', ')}` : 'waits on nothing') : '',
  ]
    .filter(Boolean)
    .join(' · ');

const PLAN_WIDE: Record<string, { title: string; detail: string; button: string; done: string }> = {
  recalculate: { title: 'Recalculate the plan', detail: 'Move each task to the dates its relationships and constraints ask for', button: 'Recalculate', done: '✓ Recalculated' },
  level: { title: 'Level the plan', detail: 'Delay tasks until nobody is asked for more than they can give', button: 'Level', done: '✓ Levelled' },
  baseline: { title: 'Save a plan baseline', detail: 'Keep the plan as it stands, to measure against later', button: 'Save baseline', done: '✓ Saved' },
  reschedule_remaining: { title: 'Reschedule remaining work', detail: 'Move work that has not started up to the status date', button: 'Reschedule', done: '✓ Rescheduled' },
};

/** The card's heading and the line under it. */
export const planActionSummary = (s: Suggestion): { title: string; detail: string } => {
  const plan = planPlanAction(s);
  if (plan.outcome === 'refused') return { title: String(s.kind), detail: plan.reason };
  const a = plan.action;
  switch (a.kind) {
    case 'plan_task':
      return { title: a.task, detail: [a.title ? `rename to ${a.title}` : '', a.column ? `move to ${a.column}` : '', scheduleSummary(a.schedule)].filter(Boolean).join(' · ') };
    case 'milestone':
      return {
        title: a.milestone ? `Milestone ${a.milestone}` : `New milestone: ${a.fields.name}`,
        detail: [
          a.fields.name !== undefined && a.milestone ? `name → ${a.fields.name}` : '',
          a.fields.date !== undefined ? (a.fields.date ? `on ${a.fields.date}` : 'no date') : '',
          a.fields.criteria ? a.fields.criteria.map((c) => `${c.metric.replace(/_/g, ' ')} ${c.target}%`).join(', ') || 'no criteria' : '',
        ]
          .filter(Boolean)
          .join(' · '),
      };
    case 'task_todo':
      return { title: a.text, detail: [`on ${a.task}`, a.due ? `due ${a.due}` : ''].filter(Boolean).join(' · ') };
    case 'todo_done':
      return { title: a.text, detail: a.done ? 'Tick it off' : 'Open it again' };
    case 'plan':
      return { title: a.action === 'baseline' && a.name ? `Save a plan baseline: ${a.name}` : PLAN_WIDE[a.action].title, detail: PLAN_WIDE[a.action].detail };
  }
};

/** The card's button, and what it says once applied. */
export const planActionLabels = (s: Suggestion): { button: string; done: string } => {
  switch (s.kind) {
    case 'plan_task':
      return { button: 'Update task', done: '✓ Updated' };
    case 'milestone':
      return text(s.milestone) ? { button: 'Change milestone', done: '✓ Changed' } : { button: '+ Add milestone', done: '✓ Added' };
    case 'task_todo':
      return { button: '+ Add to-do', done: '✓ Added' };
    case 'todo_done':
      return s.done === false ? { button: 'Reopen', done: '✓ Reopened' } : { button: 'Tick off', done: '✓ Done' };
    case 'plan': {
      const p = PLAN_WIDE[text(s.action)];
      return p ? { button: p.button, done: p.done } : { button: 'Apply', done: '✓ Applied' };
    }
    default:
      return { button: 'Apply', done: '✓ Applied' };
  }
};

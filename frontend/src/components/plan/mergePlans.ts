// Every project's plan as one plan, so the workspace's Plan page can draw
// it with the views a project's Gantt has (the outline, the network, the
// calendar, who is loaded when) instead of charts of its own. Each
// project is a top-level row, with its tasks beneath it a level down.
import type { PlanResourceLoad, PlanTask, ProjectPlan } from '../../api/client';

export interface PlanOfProject {
  id: string;
  name: string;
  plan: ProjectPlan;
}

export interface MergedPlan {
  plan: ProjectPlan;
  /** The project each row belongs to, by the row's id; a project's own row too. */
  projectOf: ReadonlyMap<string, string>;
}

/** The id of the row that stands for a project. */
export const projectRowId = (projectId: string): string => `project:${projectId}`;

const min = (a: string | null, b: string | null) => (!a ? b : !b ? a : a < b ? a : b);
const max = (a: string | null, b: string | null) => (!a ? b : !b ? a : a > b ? a : b);

/** The row that stands for a project: a parent whose dates span its plan. */
const projectRow = (p: PlanOfProject, n: number): PlanTask => {
  const top = p.plan.tasks.filter((t) => !t.parent_id);
  const s = p.plan.summary;
  return {
    id: projectRowId(p.id),
    title: p.name,
    column: '',
    assignee_type: 'user',
    assignee_id: null,
    parent_id: null,
    depth: 0,
    wbs: String(n),
    order: n,
    start: null,
    end: null,
    span_start: p.plan.start,
    span_end: p.plan.end,
    duration_days: null,
    effort_days: s.effort_days || null,
    float_days: null,
    critical: false,
    float_until: null,
    elapsed: false,
    deadline: null,
    deadline_missed: false,
    mode: p.plan.scheduling_mode,
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
    cost: s.cost,
    effort_driven: false,
    overallocated: false,
    actual_start: null,
    actual_finish: null,
    percent_complete: null,
    actual_cost: null,
    baseline: null,
    start_variance_days: null,
    finish_variance_days: null,
    earned_value: null,
    behind_schedule: false,
    critical_subtasks: s.critical > 0,
    progress: s.progress,
    done: s.tasks > 0 && s.done === s.tasks,
    late: s.late > 0,
    todos_done: 0,
    todos_total: 0,
    subtasks_done: top.filter((t) => t.done).length,
    // A project with no task yet is still a parent: it has no dates of its own to set.
    subtasks_total: Math.max(top.length, 1),
    milestone: null,
    milestone_inherited: false,
    todos: [],
    depends_on: [],
    relationships: [],
    drives: [],
    conflicts: [],
    external_links: [],
  };
};

/** One person's load across the projects: their tasks, days and cost add up; the peak is the highest any one plan asks. */
const mergeResources = (plans: PlanOfProject[]): PlanResourceLoad[] => {
  const byKey = new Map<string, PlanResourceLoad>();
  for (const p of plans) {
    for (const r of p.plan.resources) {
      const key = `${r.type}:${r.id}`;
      const was = byKey.get(key);
      if (!was) {
        byKey.set(key, { ...r, overallocations: [...r.overallocations], time_off: [...r.time_off] });
        continue;
      }
      was.tasks += r.tasks;
      was.work_days += r.work_days;
      was.cost += r.cost;
      was.peak = Math.max(was.peak, r.peak);
      was.overallocations.push(...r.overallocations);
    }
  }
  return Array.from(byKey.values());
};

/**
 * The plans of several projects as one. The calendar (working days,
 * holidays, today) is the first plan's: a workspace's projects share the
 * workspace's working days. Nothing in the result can be saved: its
 * project rows are not tasks.
 */
export const mergePlans = (plans: PlanOfProject[]): MergedPlan | null => {
  const [first] = plans;
  if (!first) return null;
  const projectOf = new Map<string, string>();
  const tasks: PlanTask[] = [];
  plans.forEach((p, i) => {
    const row = projectRow(p, i + 1);
    projectOf.set(row.id, p.id);
    tasks.push(row);
    for (const t of p.plan.tasks) {
      projectOf.set(t.id, p.id);
      tasks.push({ ...t, depth: t.depth + 1, parent_id: t.parent_id || row.id, wbs: `${i + 1}.${t.wbs}` });
    }
  });
  const sum = (pick: (s: ProjectPlan['summary']) => number) => plans.reduce((n, p) => n + pick(p.plan.summary), 0);
  const total = sum((s) => s.tasks);
  const holidays = new Map(plans.flatMap((p) => p.plan.holidays).map((h) => [h.date, h]));
  const people = new Map(plans.flatMap((p) => p.plan.people).map((x) => [x.id, x]));
  const plan: ProjectPlan = {
    ...first.plan,
    project_id: '',
    holidays: Array.from(holidays.values()),
    project_start: null,
    project_finish: null,
    budget: null,
    active_baseline_id: null,
    baseline_id: null,
    baselines: [],
    resources: mergeResources(plans),
    people: Array.from(people.values()),
    start: plans.reduce<string | null>((d, p) => min(d, p.plan.start), null),
    end: plans.reduce<string | null>((d, p) => max(d, p.plan.end), null),
    tasks,
    // Two projects can both have a "Beta": the project's name tells them apart.
    milestones: plans.flatMap((p) => p.plan.milestones.map((m) => ({ ...m, name: plans.length > 1 ? `${p.name}: ${m.name}` : m.name }))),
    dependencies: plans.flatMap((p) => p.plan.dependencies),
    metrics: [],
    summary: {
      tasks: total,
      done: sum((s) => s.done),
      late: sum((s) => s.late),
      unscheduled: sum((s) => s.unscheduled),
      progress: total ? Math.round(plans.reduce((n, p) => n + p.plan.summary.progress * p.plan.summary.tasks, 0) / total) : 0,
      critical: sum((s) => s.critical),
      effort_days: sum((s) => s.effort_days),
      cost: sum((s) => s.cost),
      overallocated: sum((s) => s.overallocated),
    },
  };
  return { plan, projectOf };
};

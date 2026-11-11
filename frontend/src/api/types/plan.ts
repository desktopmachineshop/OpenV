// The project plan, the Gantt over the board (internal/domain/plan).

/** A to-do on a task, as the plan shows it. */
export interface PlanTaskTodo {
  id: string;
  text: string;
  done: boolean;
  /** The to-do's one date, its deadline (YYYY-MM-DD). */
  due_date: string | null;
}

/** One row of the plan: a board task with its schedule and progress. */
export interface PlanTask {
  id: string;
  title: string;
  column: string;
  assignee_type: 'user' | 'agent';
  assignee_id: string | null;
  parent_id: string | null;
  depth: number;
  wbs: string;
  /** The task's own dates, YYYY-MM-DD; end is the work item's due date. */
  start: string | null;
  end: string | null;
  /** What the chart draws: the own dates widened to cover the sub-tasks. */
  span_start: string | null;
  span_end: string | null;
  /** The task's length in the plan's working days, counting its first and last; kept while it has no dates. */
  duration_days: number | null;
  /** The work in the task, in person-days. */
  effort_days: number | null;
  /** How many working days it can slip before the plan ends later or it breaks a deadline or constraint; negative when it is already past one; null with no dates. */
  float_days: number | null;
  /** On the critical path, the chain of tasks that decides when the plan ends, and not done. Never a task with sub-tasks. */
  critical: boolean;
  /** The last day the task's end can slip to before it pushes a task it drives, or the plan's end; null when it has no room. */
  float_until: string | null;
  /** The duration counts calendar days, weekends and holidays included. */
  elapsed: boolean;
  /** The day the task should be finished by; it moves nothing. */
  deadline: string | null;
  /** Not done, and it ends or has run past its deadline. */
  deadline_missed: boolean;
  /** The scheduling mode in force, and the task's own (empty: the plan's). */
  mode: PlanSchedulingMode;
  mode_own: PlanSchedulingMode | '';
  constraint_type: PlanConstraintType;
  constraint_date: string | null;
  /** Its dates do not keep to its constraint. */
  constraint_violated: boolean;
  /** The earliest and latest it can run; null with no dates. */
  early_start: string | null;
  early_finish: string | null;
  late_start: string | null;
  late_finish: string | null;
  /** How far it can slip before it moves a task it drives. */
  free_float_days: number | null;
  /** Who does the task: those set on the plan, or the board's assignee at 100%. */
  assignments: PlanAssignment[];
  fixed_cost: number;
  /** Its people's work at their rates, its fixed cost and its sub-tasks' costs. */
  cost: number;
  /** Its effort and the people on it decide its duration. */
  effort_driven: boolean;
  /** Someone on it is asked for more than their capacity while it runs. */
  overallocated: boolean;
  /** What happened: the days work really started and finished, a percentage typed by hand (it is then the progress), and what it has cost. */
  actual_start: string | null;
  actual_finish: string | null;
  percent_complete: number | null;
  actual_cost: number | null;
  /** The task as the baseline measured against has it, and how many working days later (negative: earlier) it now starts and finishes. */
  baseline: PlanBaselineTask | null;
  start_variance_days: number | null;
  finish_variance_days: number | null;
  earned_value: PlanEarnedValue | null;
  /** Less is done than the status day asks. */
  behind_schedule: boolean;
  /** The critical path runs through this task's sub-tasks. */
  critical_subtasks: boolean;
  /** A whole percentage, from the to-dos ticked off and the sub-tasks. */
  progress: number;
  done: boolean;
  late: boolean;
  todos_done: number;
  todos_total: number;
  subtasks_done: number;
  subtasks_total: number;
  /** The key of the milestone the task counts toward: its own, or one a task above it is assigned to. */
  milestone: string | null;
  /** The milestone is a task above's, not this task's own. */
  milestone_inherited: boolean;
  todos: PlanTaskTodo[];
  depends_on: string[];
  /** The tasks that drive this one, each with its type and whether the dates break it. */
  relationships: PlanRelationship[];
  /** The tasks this one drives, in the same shape. */
  drives: PlanRelationship[];
  /** The driving tasks whose relationship the dates break. */
  conflicts: string[];
  /** The task's relationships to tasks of other projects. */
  external_links: PlanExternalLink[];
}

/** A relationship to a task of another project, as the task that carries it sees it. */
export interface PlanExternalLink {
  task_id: string;
  project_id: string;
  project_name: string;
  title: string;
  start: string | null;
  end: string | null;
  done: boolean;
  type: PlanRelationshipType;
  lag_days: number;
  lag_unit: PlanLagUnit;
  /** driven_by: the other task drives this one. */
  direction: PlanRelationshipDirection;
  conflict: boolean;
}

/** A project's line of the master plan. */
export interface MasterProject {
  project_id: string;
  name: string;
  start: string | null;
  end: string | null;
  summary: ProjectPlan['summary'];
  tasks: { id: string; title: string; wbs: string; start: string | null; end: string | null; progress: number; done: boolean; late: boolean; critical: boolean }[];
  milestones: { key: string; name: string; date: string; progress: number; met: boolean; late: boolean }[];
}

/** The plans of the workspace's projects the reader can see, and the links between their tasks. */
export interface MasterPlan {
  today: string;
  start: string | null;
  end: string | null;
  projects: MasterProject[];
  links: { from_project_id: string; from_title: string; to_project_id: string; to_title: string; type: PlanRelationshipType; conflict: boolean }[];
}

/** A task as a baseline recorded it. */
export interface PlanBaselineTask {
  id: string;
  start: string | null;
  end: string | null;
  duration_days: number | null;
  effort_days: number | null;
  cost: number;
}

/** A saved baseline: the plan as it stood. */
export interface PlanBaselineInfo {
  id: string;
  name: string;
  created_at: string;
  tasks: number;
}

/** Earned value: the baseline's cost, what was planned, earned and spent by the status day, and the variances and indices from them. */
export interface PlanEarnedValue {
  bac: number;
  pv: number;
  ev: number;
  ac: number;
  sv: number;
  cv: number;
  spi: number;
  cpi: number;
  eac: number;
}

/** FS: finish to start. SS: start to start. FF: finish to finish. SF: start to finish. */
export type PlanRelationshipType = 'FS' | 'SS' | 'FF' | 'SF';

/** An offset's unit: working days, elapsed (calendar) days, or percent of the driving task's duration. */
export type PlanLagUnit = 'd' | 'ed' | '%';

/** A person or an agent on a task, for a percentage of their working time. */
export interface PlanAssignment {
  type: 'user' | 'agent';
  id: string;
  units: number;
}

/** Someone a task can be assigned to. */
export interface PlanPerson {
  type: 'user' | 'agent';
  id: string;
  name: string;
}

/** A resource with the plan's work on them. */
export interface PlanResourceLoad {
  type: 'user' | 'agent';
  id: string;
  name: string;
  /** What a day of their time costs, and the percentage of a day they can give. */
  daily_rate: number;
  capacity: number;
  tasks: number;
  work_days: number;
  cost: number;
  /** The most asked of them on any day, a percentage. */
  peak: number;
  /** The runs of working days on which more than their capacity is asked. */
  overallocations: { start: string; end: string; units: number }[];
}

/** A date constraint: none, as late as possible, start/finish no earlier/later than, must start/finish on. */
export type PlanConstraintType = 'asap' | 'alap' | 'snet' | 'snlt' | 'fnet' | 'fnlt' | 'mso' | 'mfo';

/** Placed by hand, or by the platform from relationships and constraints. */
export type PlanSchedulingMode = 'manual' | 'auto';

/** A day the plan does not work whatever its weekday. */
export interface PlanHoliday {
  date: string;
  name: string;
}

/** The plan's calendar and critical threshold; a change names the fields it sets. */
export interface PlanSettings {
  working_days: number[];
  holidays: PlanHoliday[];
  /** A task with this much float or less is critical. */
  critical_slack_days: number;
  /** The mode of every task that has none of its own. */
  scheduling_mode: PlanSchedulingMode;
  /** The day the plan starts, and the day it must finish by; empty clears one. */
  project_start: string | null;
  project_finish: string | null;
  /** From its start, tasks go as soon as possible; from its finish, as late. */
  schedule_from: 'start' | 'finish';
  /** Each chain of tasks has a critical path of its own. */
  multiple_critical_paths: boolean;
  /** The money costs are in, up to three letters; and what the plan may cost (0 clears it). */
  currency: string;
  budget: number | null;
  /** The day progress is reported as of (unset: today), and the baseline chosen to measure against (unset: the newest). */
  status_date: string | null;
  active_baseline_id: string | null;
}

export interface PlanRelationship {
  task_id: string;
  type: PlanRelationshipType;
  /** The offset: what the driven task keeps after what the type asks for; negative leads. */
  lag_days: number;
  lag_unit: PlanLagUnit;
  conflict: boolean;
}

export interface PlanCriterion {
  metric: string;
  /** A percentage from 1 to 100. */
  target: number;
}

export interface PlanCriterionResult extends PlanCriterion {
  label: string;
  actual: number;
  count: number;
  total: number;
  met: boolean;
  /** How far the criterion is to its target, as a percentage. */
  percent: number;
}

export interface PlanMilestone {
  /** The milestone's id, or "release:<id>" for a release's. */
  key: string;
  kind: 'release' | 'custom';
  /** Empty for a release milestone still on the default criteria. */
  id: string;
  release_id: string | null;
  name: string;
  description: string;
  date: string | null;
  release_status?: string;
  default_criteria: boolean;
  criteria: PlanCriterionResult[];
  progress: number;
  met: boolean;
  late: boolean;
  /** The ids of the tasks that count toward the milestone, in outline order. */
  tasks: string[];
}

export interface PlanMetric {
  key: string;
  label: string;
  description: string;
}

export interface PlanMetricValue {
  metric: string;
  label: string;
  count: number;
  total: number;
  percent: number;
}

export interface ProjectPlan {
  project_id: string;
  /** The days of the week the plan works, as ISO weekday numbers: Monday is 1, Sunday 7. */
  working_days: number[];
  holidays: PlanHoliday[];
  critical_slack_days: number;
  scheduling_mode: PlanSchedulingMode;
  project_start: string | null;
  project_finish: string | null;
  schedule_from: 'start' | 'finish';
  multiple_critical_paths: boolean;
  currency: string;
  budget: number | null;
  status_date: string | null;
  active_baseline_id: string | null;
  /** The day progress is measured at, the baseline measured against, and every baseline saved. */
  status_day: string;
  baseline_id: string | null;
  baselines: PlanBaselineInfo[];
  earned_value: PlanEarnedValue;
  resources: PlanResourceLoad[];
  people: PlanPerson[];
  today: string;
  start: string | null;
  end: string | null;
  tasks: PlanTask[];
  milestones: PlanMilestone[];
  dependencies: { work_item_id: string; depends_on_id: string; type: PlanRelationshipType }[];
  metrics: PlanMetricValue[];
  metric_catalogue: PlanMetric[];
  summary: { tasks: number; done: number; late: number; unscheduled: number; progress: number; critical: number; effort_days: number; cost: number; overallocated: number };
}

/** Which way a relationship made from a task runs: it drives the other, or is driven by it. */
export type PlanRelationshipDirection = 'drives' | 'driven_by';

/** A field left out stays as it is; an empty date or parent clears it. */
export interface PlanScheduleInput {
  start_date?: string;
  due_date?: string;
  /** A task keeps its duration: one date sent alone moves the other. 0 clears it. */
  duration_days?: number;
  /** Person-days; 0 clears it. */
  effort_days?: number;
  /** Count the duration in calendar days. */
  elapsed?: boolean;
  /** YYYY-MM-DD; empty clears it. */
  deadline?: string;
  /** The task's own mode; empty takes the plan's. */
  mode?: PlanSchedulingMode | '';
  constraint_type?: PlanConstraintType;
  constraint_date?: string;
  /** Replaces who the task is assigned to on the plan. */
  assignments?: PlanAssignment[];
  fixed_cost?: number;
  effort_driven?: boolean;
  /** YYYY-MM-DD; empty clears it. */
  actual_start?: string;
  actual_finish?: string;
  /** 0 to 100; -1 goes back to the counted progress. */
  percent_complete?: number;
  /** -1 clears it. */
  actual_cost?: number;
  parent_id?: string;
  /** The sibling the task goes straight after in the outline; empty makes it the first. */
  after_id?: string;
  /** The key of the milestone the task is assigned to; empty takes it off. */
  milestone?: string;
  /** Replaces the task's relationships to the tasks that drive it. */
  relationships?: { task_id: string; type: PlanRelationshipType; lag_days?: number; lag_unit?: PlanLagUnit }[];
  /** The earlier form: ids, each keeping the type it has. */
  depends_on?: string[];
}

export interface PlanMilestoneInput {
  name?: string;
  description?: string;
  target_date?: string;
  criteria?: PlanCriterion[];
}

/** A stored milestone, as its writes answer it. */
export interface StoredPlanMilestone {
  id: string;
  project_id: string;
  release_id: string | null;
  name: string;
  description: string;
  target_date: string | null;
  criteria: PlanCriterion[];
}

export type PlanExportFormat = 'pdf' | 'excel' | 'csv' | 'msproject' | 'json';

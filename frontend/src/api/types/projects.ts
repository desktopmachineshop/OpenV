// The types api/projects.ts sends and receives.
// Projects: their settings, downloads, baselines and baseline diffs,
// templates, artifact types and link rules, attribute definitions, the
// product profile, and the work items on a project's board.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

import type { Artifact, Link } from './artifacts';
import type { Attachment } from './attachments';

export interface Project {
  id: string;
  org_id: string;
  name: string;
  description: string;
  // How agent runs in this project authenticate with their AI provider:
  // 'user-account' (each member's local CLI sign-in) or 'api-key'
  // (workspace API key; overrides local sign-ins but still runs via the
  // member's OpenV connector/runner).
  agent_auth: 'user-account' | 'api-key';
  /**
   * The project this one refines (REQ-144): a subsystem or supplier project
   * under the system it belongs to. '' for a top-level project.
   */
  parent_project_id: string;
  created_at: string;
  updated_at: string;
}

/**
 * A party a project recognises as an owner (REQ-147): an organisation, team
 * or person, matched by name. The workspace's own company is always first
 * and marked default.
 */
export interface Party {
  name: string;
  note?: string;
  default?: boolean;
}

export interface Baseline {
  id: string;
  project_id: string;
  name: string;
  created_at: string;
  /** The account that captured it. Absent for baselines taken before
   *  authorship was recorded, and for one captured by an automation. */
  created_by?: string;
  /** That account's display name, resolved server-side so a reader never
   *  sees a bare id. Absent whenever created_by is. */
  created_by_name?: string;
}

export interface Template {
  id: string;
  key?: string;
  name: string;
  description: string;
  source?: string; // "database" or "file"
  is_default: boolean;
  created_at: string;
}

export interface ProjectExport {
  exported_at: string;
  version: string;
  project_id: string;
  project_name: string;
  project_description: string;
  artifacts: Artifact[];
  links: Link[];
  attachments: Attachment[];
}

// What a project offers a download: the sections a reader can narrow to, the
// artifact types it holds, and the attachment categories actually attached.
// Served by /download/options so a chooser never offers a filter that would
// come back empty.
export interface DownloadOptions {
  sections: DownloadSection[];
  types: { type: string; count: number }[];
  attachments: { category: string; count: number; bytes: number }[];
  /**
   * The attribute keys present in the project, standard keys first. Only the
   * PDF and Word documents honour a field choice; the data formats carry every
   * attribute regardless.
   */
  fields?: DownloadField[];
  /** The owners the project's artifacts name, most artifacts first (REQ-148). */
  owners?: { owner: string; count: number }[];
  /** The presets a reader can start from. */
  templates?: DownloadTemplate[];
  /** What the document holds when no switch is sent. */
  defaults?: DownloadContent;
}

export interface DownloadSection {
  id: string;
  ref?: string;
  number?: string;
  title: string;
  artifacts: number;
}

/** One attribute key a document can show or hide. */
export interface DownloadField {
  key: string;
  label: string;
  /** How many artifacts carry a value for it. */
  count: number;
  /** A definition or discovered key, rather than one of the standard ones. */
  custom: boolean;
}

/** What goes into the PDF and Word documents beyond the artifacts themselves. */
export interface DownloadContent {
  /** The preset the reader started from, recorded on the cover. */
  template?: string;
  /** Incoming and outgoing links under each artifact. */
  traceability: boolean;
  /** Figures embedded in the document with their captions. */
  figures: boolean;
  /** A table of contents. */
  toc: boolean;
  /** Every attribute, whatever `fields` says. */
  all_fields: boolean;
  /** When `all_fields` is false, the keys to show. Empty means none. */
  fields?: string[];
  /** The latest result per test case, plus a test-run appendix. */
  test_results: boolean;
  /** A verification rollup per requirement, with a coverage summary and gaps. */
  vv_status: boolean;
}

/** A preset a reader can start a document from. */
export interface DownloadTemplate {
  key: string;
  name: string;
  description: string;
  /** The artifact types the preset narrows to. Absent means all of them. */
  types?: string[];
  content: DownloadContent;
}

/** What a download contains. Empty lists mean "everything". */
export interface DownloadSelection {
  sections: string[];
  /**
   * Types to keep. Empty means every type, or a preset's types when a
   * template is named; `['all']` every type whatever the preset keeps.
   */
  types: string[];
  /** Owners to keep; empty means everyone (REQ-148). */
  owners: string[];
  includeHeadings: boolean;
  attachments: string[];
  /** The preset the reader started from, or '' when none was chosen. */
  template: string;
  traceability: boolean;
  figures: boolean;
  toc: boolean;
  testResults: boolean;
  vvStatus: boolean;
  /** The attribute keys to show. Undefined means every one of them. */
  fields?: string[];
}

export type DownloadFormat = 'json' | 'csv' | 'excel' | 'reqif' | 'pdf' | 'docx';

/** One side of a baseline comparison: a baseline, or the live project (id "live"). */
export interface BaselineDiffRef {
  id: string;
  name: string;
}

/** An artifact present on only one side of a baseline comparison. */
export interface BaselineDiffArtifact {
  id: string;
  type: string;
  title: string;
}

/** An artifact present on both sides with at least one tracked field changed. */
export interface BaselineDiffModified {
  id: string;
  type: string;
  old_title: string;
  new_title: string;
  title_changed: boolean;
  body_changed: boolean;
  type_changed: boolean;
  status_changed: boolean;
  parent_changed: boolean;
}

/** A link present on only one side of a baseline comparison. */
export interface BaselineDiffLink {
  from_id: string;
  to_id: string;
  type: string;
  from_title: string;
  to_title: string;
}

/** Changes in the direction base → target (added = present only in target). */
export interface BaselineDiff {
  base: BaselineDiffRef;
  target: BaselineDiffRef;
  added: BaselineDiffArtifact[];
  removed: BaselineDiffArtifact[];
  modified: BaselineDiffModified[];
  links_added: BaselineDiffLink[];
  links_removed: BaselineDiffLink[];
}

export interface ArtifactTypeDef {
  value: string;
  label: string;
  description: string;
  color: string;
}

export interface LinkTypeRule {
  type: string;
  label: string;
  inverseLabel: string;
  allowedFromTypes: string[];
  allowedToTypes: string[];
  description: string;
}

export type AttributeDataType = 'text' | 'number' | 'date' | 'enum' | 'boolean';

// A configurable typed attribute definition (issue #219). Exactly one of
// org_id (org-wide) or project_id (project-scoped) is set. Values live in the
// artifact's attributes map under `key`.
export interface AttributeDefinition {
  id: string;
  org_id: string | null;
  project_id: string | null;
  key: string;
  label: string;
  data_type: AttributeDataType;
  enum_values: string[];
  // Artifact type key this applies to, or '' for all types.
  applies_to_type: string;
  required: boolean;
  sort_order: number;
  created_at: string;
}

export interface ProductProfile {
  project_id: string;
  vision: string;
  problem_statement: string;
  target_users: string;
  constraints: Record<string, any>[];
  success_metrics: Record<string, any>[];
  settings: Record<string, any>;
  updated_at: string;
}

export interface WorkItem {
  id: string;
  project_id: string;
  title: string;
  description: string;
  column: string;
  sort_order: number;
  assignee_type: 'user' | 'agent';
  assignee_id?: string | null;
  agent_run_id?: string | null;
  artifact_ids: string[];
  due_date?: string | null;
  /** The note this to-do was raised from, when it was raised from one. */
  source_chatter_id?: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkItemActivity {
  id: string;
  work_item_id: string;
  kind: string;
  actor: string;
  content: string;
  payload: Record<string, any>;
  created_at: string;
}

/**
 * A to-do (issue #731): one paragraph to tick off, kept off the board. A
 * project's, or a personal one (project_id null) that only its writer sees.
 */
export interface Todo {
  id: string;
  org_id: string;
  project_id: string | null;
  text: string;
  done: boolean;
  done_at: string | null;
  /** Why it was ticked off when not simply done: "promoted" to a board task. */
  done_note: string;
  assignee_id: string | null;
  due_date: string | null;
  source_artifact_id: string | null;
  source_chatter_id: string | null;
  promoted_work_item_id: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

/** What a to-do is made from; the route says whose it is. */
export interface TodoInput {
  text: string;
  assignee_id?: string | null;
  due_date?: string | null;
  source_chatter_id?: string | null;
  source_artifact_id?: string | null;
}

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
  /** How many days it can slip before the plan's last task ends later; null with no dates. */
  float_days: number | null;
  /** On the critical path, the chain of tasks that decides when the plan ends, and not done. Never a task with sub-tasks. */
  critical: boolean;
  /** The last day the task's end can slip to before it pushes a task it drives; null when it drives none or has no room. */
  float_until: string | null;
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
}

/** FS: finish to start. SS: start to start. FF: finish to finish. */
export type PlanRelationshipType = 'FS' | 'SS' | 'FF';

export interface PlanRelationship {
  task_id: string;
  type: PlanRelationshipType;
  /** The offset: working days the driven task keeps after what the type asks for; negative leads. */
  lag_days: number;
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
  today: string;
  start: string | null;
  end: string | null;
  tasks: PlanTask[];
  milestones: PlanMilestone[];
  dependencies: { work_item_id: string; depends_on_id: string; type: PlanRelationshipType }[];
  metrics: PlanMetricValue[];
  metric_catalogue: PlanMetric[];
  summary: { tasks: number; done: number; late: number; unscheduled: number; progress: number; critical: number; effort_days: number };
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
  parent_id?: string;
  /** The key of the milestone the task is assigned to; empty takes it off. */
  milestone?: string;
  /** Replaces the task's relationships to the tasks that drive it. */
  relationships?: { task_id: string; type: PlanRelationshipType }[];
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

export type PlanExportFormat = 'pdf' | 'excel' | 'csv' | 'json';

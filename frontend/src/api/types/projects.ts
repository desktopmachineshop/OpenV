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
  assignee_type: 'user' | 'agent' | 'team';
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

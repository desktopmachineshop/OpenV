// The types api/vv.ts sends and receives.
// Verification and validation: test runs and results, execution methods,
// coverage, the traceability matrix, impact analysis and evidence bundles.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

export interface TestRun {
  id: string;
  project_id: string;
  name: string;
  description: string;
  baseline_id?: string | null;
  /** The baseline `baseline_id` names was deleted since; the run keeps the reference as history. */
  baseline_deleted?: boolean;
  status: 'in-progress' | 'completed' | 'aborted';
  started_at: string;
  completed_at?: string | null;
}

export interface TestResult {
  id: string;
  run_id: string;
  test_case_id: string;
  test_case_version: number;
  status: 'pass' | 'fail' | 'blocked' | 'not-run';
  notes: string;
  evidence: string[];
  executed_at?: string | null;
  // Set when an agent run produced this result, so reviewers can tell
  // agent-executed evidence from human-executed.
  executed_by_agent_run_id?: string | null;
}

// How a test case can be carried out. Only 'automated' cases may be executed
// by an agent; 'manual' needs a person and 'physical' needs hardware or a rig.
// Stored on the test-case artifact's `execution_method` attribute; an unset
// value means 'automated'.
export type ExecutionMethod = 'automated' | 'manual' | 'physical';

export interface CoverageEntry {
  requirement_id: string;
  title: string;
  verification_method: string;
  verification_status: string;
  test_case_ids: string[];
  latest_results: Record<string, string>;
  rollup: string;
  /**
   * Child-project requirements refining this one (REQ-146), each with its
   * own rollup; flow_down is the worst of them, and via_refinements says the
   * rollup came from them because the requirement has no evidence of its own.
   */
  refinements?: Refinement[];
  flow_down?: string;
  via_refinements?: boolean;
}

export interface Refinement {
  requirement_id: string;
  project_id: string;
  project_name: string;
  ref: string;
  title: string;
  rollup: string;
}

export interface CoverageReport {
  project_id: string;
  entries: CoverageEntry[];
  summary: Record<string, number>;
}

export interface MatrixRow {
  requirement_id: string;
  title: string;
  user_need_ids: string[];
  design_ids: string[];
  test_case_ids: string[];
  latest_results: Record<string, string>;
  hazard_ids: string[];
}

export type ImpactDirection = 'downstream' | 'upstream' | 'both';

export interface ImpactNode {
  artifact_id: string;
  title: string;
  type: string;
  distance: number;
  via: string;
  path: string[];
}

export interface ImpactGroup {
  type: string;
  count: number;
  nodes: ImpactNode[];
}

export interface ImpactReport {
  project_id: string;
  artifact_id: string;
  direction: ImpactDirection;
  downstream: ImpactGroup[];
  upstream: ImpactGroup[];
  total: number;
}

// An evidence bundle is one physical or manual capture session: what was done,
// when, by whom, and the files it produced. It belongs to the project rather
// than to any run, because one long run on a rig commonly answers several test
// cases at once — results cite it.
export interface EvidenceFile {
  id: string;
  bundle_id: string;
  filename: string;
  mime_type: string;
  file_size: number;
  /** Recorded at upload, so a download can be checked against the record. */
  sha256: string;
  uploaded_by?: string | null;
  created_at: string;
}

/** One result's claim on one bundle. Carries display fields so a citation can
 *  be rendered without resolving four more ids. */
export interface EvidenceCitation {
  id: string;
  bundle_id: string;
  test_result_id: string;
  note: string;
  created_at: string;
  bundle_ref?: string;
  bundle_title?: string;
  test_case_id?: string;
  test_case_title?: string;
  test_case_ref?: string;
  run_id?: string;
  run_name?: string;
}

export interface EvidenceBundle {
  id: string;
  project_id: string;
  /** Citable, per project: "EVD-1". Never reissued. */
  ref: string;
  title: string;
  summary: string;
  /** When the test was carried out, which is not when it was uploaded. */
  captured_at?: string | null;
  /** Free text: the person on the rig is often not an OpenV user. */
  captured_by: string;
  conditions: Record<string, unknown>;
  created_by?: string | null;
  created_at: string;
  updated_at: string;
  /** Present on a single-bundle fetch only; the list carries the counts. */
  files?: EvidenceFile[];
  citations?: EvidenceCitation[];
  file_count: number;
  total_size: number;
}

export interface EvidenceBundleInput {
  title: string;
  summary?: string;
  captured_at?: string | null;
  captured_by?: string;
  conditions?: Record<string, unknown>;
}

/** A project's target release ("MVP", "V1.0"); see internal/domain/targetreleases. */
export type TargetReleaseStatus = 'planned' | 'in_progress' | 'released';

export interface TargetRelease {
  id: string;
  project_id: string;
  name: string;
  description: string;
  /** YYYY-MM-DD, or null when none is set. */
  target_date: string | null;
  status: TargetReleaseStatus;
  position: number;
  released_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface TargetReleaseInput {
  name?: string;
  description?: string;
  /** An empty string clears the date on an update. */
  target_date?: string;
  status?: TargetReleaseStatus;
}

/** The range of releases an artifact targets, as the server resolved it. */
export interface ReleaseRange {
  from: string;
  until?: string;
  /** The ancestor the range is inherited from, when it is. */
  inherited_from?: string;
}

export interface ReleaseSummaryCounts {
  total: number;
  introduced: number;
  approved: number;
  verified: number;
  ready: number;
  by_status: Record<string, number>;
  by_rollup: Record<string, number>;
}

export interface ReleaseOverview {
  releases: { release: TargetRelease; summary: ReleaseSummaryCounts }[];
  unassigned: number;
  unassigned_ids: string[];
}

export interface ReleaseProgressEntry {
  requirement_id: string;
  ref: string;
  title: string;
  status: string;
  rollup: string;
  range: ReleaseRange;
  introduced: boolean;
  test_case_ids: string[];
  latest_results: Record<string, string>;
}

export interface ReleaseBurnUpPoint {
  date: string;
  scope: number;
  verified: number;
}

export interface ReleaseProgress {
  release: TargetRelease;
  summary: ReleaseSummaryCounts;
  entries: ReleaseProgressEntry[];
  burn_up?: ReleaseBurnUpPoint[];
}

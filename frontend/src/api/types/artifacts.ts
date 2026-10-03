// The types api/artifacts.ts sends and receives.
// Artifacts, trace links, review rounds, the chatter feed, requirement
// quality rules and findings, search and duplicate detection.
// Code outside src/api imports these from api/client, which re-exports
// this module (refactor plan F1, K12).

export type ArtifactStatus = 'draft' | 'in_review' | 'approved' | 'superseded';

export interface Artifact {
  id: string;
  project_id: string;
  parent_id?: string | null;
  type: string;
  // Stable address, minted once per project and never reissued — this is what
  // a reader cites ("REQ-12"). Constant across versions and reordering.
  ref?: string;
  // Derived section number for headings ("1.2"). Position-dependent, so it
  // changes when the document is reordered; served only when the caller asks
  // for doc_numbers=1. Never a citation — that is what ref is for.
  doc_number?: string;
  title: string;
  body: string;
  sort_order?: number;
  // Review state: draft | in_review | approved | superseded.
  // First-class column; attributes.status is only a deprecated mirror.
  status?: ArtifactStatus;
  attributes: Record<string, any>;
  version: number;
  valid_from: string;
  valid_to: string | null;
  created_at: string;
  updated_at: string;
}

export interface Link {
  id: string;
  from_id: string;
  to_id: string;
  type: string;
  // True when the content of a linked artifact changed after the link was
  // made; cleared by an explicit confirm or by re-approving the artifact.
  // Optional: absent on legacy links_snapshot copies.
  suspect?: boolean;
  attributes: Record<string, any>;
  version: number;
  created_at: string;
  updated_at: string;
}

/** The far end of a link that crosses into another project (REQ-145). */
export interface LinkedArtifact {
  id: string;
  project_id: string;
  project_name: string;
  ref: string;
  type: string;
  title: string;
  status: string;
}

// A suspect link enriched with its endpoints' titles/types, as returned by
// the review-queue endpoint (issue #183). Distinct from Link: it is a
// read-only projection joined against both artifacts server-side.
export interface SuspectLink {
  id: string;
  type: string;
  from_id: string;
  from_title: string;
  from_type: string;
  to_id: string;
  to_title: string;
  to_type: string;
  updated_at: string;
}

// The review-queue payload: the two things awaiting a reviewer's attention.
export interface ReviewQueue {
  suspect_links: SuspectLink[];
  in_review_artifacts: Artifact[];
}

// What one run of a project's review process did. The counts account for
// every artifact in the project, so the UI can report "12 sent for review,
// 30 already approved" from the response alone.
export interface ReviewRoundResult {
  moved: Artifact[];
  already_in_review: number;
  approved: number;
  superseded: number;
  out_of_scope: number;
  types: string[];
}

export interface ChatterEntry {
  id: string;
  artifact_id: string;
  message: string;
  is_auto_entry: boolean;
  entry_type: string;
  // created_by is the authoring user's id (omitted for agent/system entries);
  // author_name is the display label resolved at write time (user name/email
  // or agent name), blank on pre-authorship rows.
  created_by?: string;
  author_name?: string;
  created_at: string;
  updated_at: string;
  // Resolved by the API when the feed is read, never stored: who the note's
  // @names address, and the to-do raised from it with its status as it
  // stands now.
  mentions?: NoteMention[];
  todo?: NoteTodo;
}

export interface NoteMention {
  user_id: string;
  name: string;
}

export interface NoteTodo {
  work_item_id: string;
  title: string;
  /** The board column the to-do sits in. */
  status: string;
  assignee_id?: string | null;
  assignee_name?: string;
}

// --- Requirement quality linting (issue #217) ---

export interface QualityFinding {
  rule: string;
  severity: 'error' | 'warning' | 'info';
  message: string;
  start: number;
  end: number;
  match?: string;
}

export interface QualityScore {
  artifact_id: string;
  title: string;
  type: string;
  score: number; // 0-100, higher is better
  band: 'good' | 'fair' | 'poor';
  findings: QualityFinding[];
}

export interface QualityReport {
  project_id: string;
  entries: QualityScore[];
  summary: Record<string, number>; // band -> count
}

// Requirement quality rule sets (workspace house style, overridable per
// project). `effective` is what the linter and agents use; `workspace` and
// `project` are the overrides that produced it, either of them null when that
// level sets nothing. `catalog` carries the vocabulary the editor renders.
export type QualityConvention = 'shall' | 'rfc2119';
export type QualitySeverity = 'error' | 'warning' | 'info' | 'off';

export interface QualityRuleSet {
  convention?: QualityConvention | '';
  severities?: Record<string, QualitySeverity>;
}

export interface QualityRulesCatalog {
  conventions: QualityConvention[];
  rules: string[];
  severities: QualitySeverity[];
  defaults: Required<QualityRuleSet>;
  labels: Record<string, string>;
}

export interface QualityRules {
  effective: Required<QualityRuleSet>;
  workspace: QualityRuleSet | null;
  project: QualityRuleSet | null;
  summary: string;
  catalog: QualityRulesCatalog;
}

// One row of the global artifact search (GET /api/v1/search). Results are
// already scoped server-side to projects the caller can access.
export interface SearchHit {
  artifact_id: string;
  project_id: string;
  project_name: string;
  type: string;
  title: string;
  snippet: string;
  // The artifact's stable short ref ("REQ-30"), so a result can be identified
  // by the address people cite it with. Absent on semantic hits, which are
  // read from the vector store rather than the artifact rows.
  ref?: string;
  // Semantic-similarity score (0..1); present only for semantic/hybrid hits.
  score?: number;
}

// Ranking path for GET /api/v1/search. 'keyword' is the original trigram match;
// 'semantic' and 'hybrid' use artifact embeddings and fall back to keyword when
// embeddings are not configured (see mode_used in the response).
export type SearchMode = 'keyword' | 'semantic' | 'hybrid';

// Envelope returned by GET /api/v1/search. mode_used reports which path
// actually ran — it differs from the requested mode when a semantic/hybrid
// request degraded to keyword because embeddings are unavailable.
export interface SearchResponse {
  mode_used: SearchMode;
  hits: SearchHit[];
}

// One candidate-duplicate pairing from GET /api/v1/projects/{id}/duplicates.
export interface DuplicatePair {
  artifact_id: string;
  artifact_title: string;
  artifact_type: string;
  other_id: string;
  other_title: string;
  other_type: string;
  similarity: number;
}

export interface DuplicatesResponse {
  enabled: boolean;
  note?: string;
  pairs: DuplicatePair[];
}

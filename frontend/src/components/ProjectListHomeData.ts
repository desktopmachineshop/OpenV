// What needs a member and what happened, built from reads the app already
// makes: the review queue, the open test runs, coverage and comments for the
// needs list, and domain events for the audit trail. The project Home
// (views/ProjectHomeData.ts) builds them for one project; the workspace Home
// on the Projects page (ProjectListHome) builds them for every project the
// member can see.
import type { Artifact, CoverageReport, DomainEvent, ReviewQueue, TestResult, TestRun, Todo } from '../api/client';

// The audit trail's wording lives in activityEvents, shared with the
// project's Activity log (#734); it is re-exported here for the Homes.
export {
  describeEvent,
  eventTarget,
  whenLabel,
  VERBS,
  type ActivityItem,
  type ActivityTone,
} from './activityEvents';

export type TodoKind = 'approve' | 'suspect' | 'test run' | 'failing' | 'comment' | 'to-do';

export interface TodoItem {
  key: string;
  kind: TodoKind;
  ref: string;
  title: string;
  meta: string;
  /** The row's own action, a link: its label says what it does ("Review", "Continue", "Open"). */
  action: string;
  to: string;
  /** Where the row itself opens: the artifact it names (a link's source), or the run. */
  open: string;
  /** A suspect link's id: the row confirms or removes it in place (#719). */
  linkId?: string;
  /**
   * A comment's artifact and when it was written: with read state on
   * (comment-reads), the row marks the artifact's comments read in place.
   */
  comment?: { artifactId: string; at: string };
}

export const percent = (part: number | undefined, total: number | undefined): number | null =>
  total ? Math.round(((part || 0) / total) * 100) : null;

/** A project's requirements by their verification coverage, from its coverage report. */
export interface CoverageFigures {
  /** Requirements in the project. */
  total: number;
  /** Requirements with something behind them: a test case, or a manual attestation. */
  traced: number;
  /** Requirements whose evidence passes, or that are verified by hand. */
  verified: number;
  /** Requirements whose latest evidence fails. */
  failing: number;
}

// Rollups that mean nothing is behind a requirement yet (vv/coverage.go).
const UNTRACED = new Set(['uncovered', 'method-missing']);
const VERIFIED = new Set(['pass', 'verified-manually']);

/**
 * The coverage report's summary counts requirements by ROLLUP (rollup ->
 * count, internal/domain/vv/coverage.go), not by covered and verified
 * totals: read as {total, covered, verified}, every project looked as if it
 * had no requirements (#720). Null without a report.
 */
export const coverageFigures = (report: Pick<CoverageReport, 'summary' | 'entries'> | null | undefined): CoverageFigures | null => {
  if (!report) return null;
  const counts: Record<string, number> = {};
  const summary = report.summary && typeof report.summary === 'object' ? report.summary : null;
  if (summary && Object.keys(summary).length > 0) {
    for (const [rollup, n] of Object.entries(summary)) counts[rollup] = (counts[rollup] || 0) + (Number(n) || 0);
  } else {
    for (const e of Array.isArray(report.entries) ? report.entries : []) counts[e.rollup] = (counts[e.rollup] || 0) + 1;
  }
  let total = 0;
  let traced = 0;
  let verified = 0;
  for (const [rollup, n] of Object.entries(counts)) {
    total += n;
    if (!UNTRACED.has(rollup)) traced += n;
    if (VERIFIED.has(rollup)) verified += n;
  }
  return { total, traced, verified, failing: counts.fail || 0 };
};

/** How long ago, briefly: "5m", "2h", "3d". */
export const agoLabel = (iso: string, now: Date = new Date()): string => {
  const ms = now.getTime() - new Date(iso).getTime();
  if (!Number.isFinite(ms)) return '';
  const min = Math.max(0, Math.round(ms / 60000));
  if (min < 60) return `${min}m`;
  const h = Math.round(min / 60);
  return h < 48 ? `${h}h` : `${Math.round(h / 24)}d`;
};

interface TodoSources {
  projectId: string;
  review: ReviewQueue | null;
  runs: { run: TestRun; results: TestResult[] }[];
  testCases: number;
  coverage: CoverageReport | null;
  comments: DomainEvent[];
  byId: Map<string, Artifact>;
  now?: Date;
  /** Read state is on: each comment row carries what Mark read needs. */
  readState?: boolean;
}

const refOf = (byId: Map<string, Artifact>, id: string): string => byId.get(id)?.ref || '';

/** What waits on the team, in the order the design lists it. */
export const buildTodo = ({ projectId, review, runs, testCases, coverage, comments, byId, now, readState }: TodoSources): TodoItem[] => {
  const base = `/projects/${projectId}`;
  const open = (id: string) => `${base}/requirements?artifact=${id}`;
  const items: TodoItem[] = [];
  for (const a of review?.in_review_artifacts || []) {
    items.push({
      key: `approve-${a.id}`,
      kind: 'approve',
      ref: a.ref || '',
      title: a.title,
      meta: `In review · ${agoLabel(a.updated_at, now)}`,
      action: 'Review',
      to: open(a.id),
      open: open(a.id),
    });
  }
  for (const l of review?.suspect_links || []) {
    const to = refOf(byId, l.to_id);
    items.push({
      key: `suspect-${l.id}`,
      kind: 'suspect',
      ref: refOf(byId, l.from_id) || l.from_title,
      title: `${l.type} ${to ? `${to} ` : ''}${l.to_title}`,
      meta: 'An end changed after linking · confirm or remove the link',
      action: 'Review',
      to: `${base}/review`,
      open: open(l.from_id),
      linkId: l.id,
    });
  }
  for (const { run, results } of runs) {
    if (run.status !== 'in-progress') continue;
    const done = results.filter((r) => r.status !== 'not-run').length;
    items.push({
      key: `run-${run.id}`,
      kind: 'test run',
      ref: '',
      title: run.name,
      meta: `${Math.max(0, testCases - done)} of ${testCases} test cases left · started ${agoLabel(run.started_at, now)} ago`,
      action: 'Continue',
      to: `${base}/vv/runs/${run.id}`,
      open: `${base}/vv/runs/${run.id}`,
    });
  }
  const failing = new Map<string, string[]>();
  for (const e of coverage?.entries || []) {
    for (const [tc, status] of Object.entries(e.latest_results || {})) {
      if (status === 'fail') failing.set(tc, [...(failing.get(tc) || []), e.requirement_id]);
    }
  }
  for (const [tc, reqs] of failing) {
    const a = byId.get(tc);
    items.push({
      key: `failing-${tc}`,
      kind: 'failing',
      ref: a?.ref || '',
      title: a?.title || 'Test case',
      meta: `Last result fail · verifies ${reqs.map((r) => refOf(byId, r) || 'a requirement').join(', ')}`,
      action: 'Open',
      to: open(tc),
      open: open(tc),
    });
  }
  for (const c of comments) {
    const id = String(c.payload?.artifact_id || '');
    const a = byId.get(id);
    items.push({
      key: `comment-${c.id}`,
      kind: 'comment',
      ref: a?.ref || '',
      title: a?.title || c.entity_name || '',
      meta: `${c.actor_name || 'Someone'}: ${String(c.payload?.message || '')}`,
      action: 'Open',
      to: `${open(id)}&notes=1`,
      open: `${open(id)}&notes=1`,
      ...(readState && id ? { comment: { artifactId: id, at: c.created_at } } : {}),
    });
  }
  return items;
};


/**
 * A member's open to-dos (issue #731) as Needs you rows: each opens the
 * to-do in its project's list, or My to-dos for a personal one, soonest due
 * first as the API lists them.
 */
export const todoNeeds = (todos: Todo[], projectNames: Map<string, string>, now: Date = new Date()): (TodoItem & { project: string; projectId: string })[] =>
  todos
    .filter((t) => !t.done && (!t.project_id || projectNames.has(t.project_id)))
    .map((t) => {
      const to = t.project_id ? `/projects/${t.project_id}/todos?todo=${t.id}` : `/org/todos?todo=${t.id}`;
      const due = t.due_date ? Date.parse(t.due_date) : NaN;
      const meta = Number.isNaN(due)
        ? 'To-do'
        : due < now.getTime()
          ? `Overdue · due ${t.due_date!.slice(0, 10)}`
          : `Due ${t.due_date!.slice(0, 10)}`;
      return {
        key: `todo-${t.id}`,
        kind: 'to-do' as const,
        ref: '',
        title: t.text,
        meta,
        action: 'Open',
        to,
        open: to,
        project: t.project_id ? projectNames.get(t.project_id) || '' : 'Personal',
        projectId: t.project_id || '',
      };
    });

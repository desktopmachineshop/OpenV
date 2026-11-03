import {
  artifactAPI,
  eventsAPI,
  proposalsAPI,
  reviewAPI,
  vvAPI,
  type Artifact,
  type DomainEvent,
  type Project,
  type Proposal,
} from '../api/client';
import { buildTodo, describeEvent, percent, type ActivityItem, type TodoItem } from './ProjectListHomeData';
import type { ProjectFigures } from './ProjectListFigures';

// The workspace Home on the Projects page (the Home design, redesign-pages):
// what needs the member across every project they can see, the workspace's
// audit trail, and each project's size and traced and verified share for the
// project picker below them.
//
// The audit trail and the comments are one workspace-wide read each (events
// without a project filter, which a member who is not an admin sees only for
// the projects they belong to). The rest is per project, a few projects at a
// time: the review queue, the test runs, coverage and the artifact count. A
// project's artifacts are read only when its needs list must name something
// in it (a failing test case, a comment, a run's test cases). Every read is
// settled on its own, so one that fails leaves its part empty.

export interface WorkspaceTodo extends TodoItem {
  project: string;
}

export interface WorkspaceHomeData {
  todo: WorkspaceTodo[];
  activity: (ActivityItem & { project: string })[];
  figures: Record<string, ProjectFigures & { failing: number | null }>;
  proposals: Proposal[];
  totals: { artifacts: number | null; traced: number | null; verified: number | null; failing: number };
}

const BATCH = 4;
const settled = <T>(r: PromiseSettledResult<{ data: T }>): T | null => (r.status === 'fulfilled' ? r.value.data : null);
/** A list read's answer, or none: an answer that is not a list counts as empty. */
const list = <T>(r: PromiseSettledResult<{ data: T[] }>): T[] => {
  const d = settled(r);
  return Array.isArray(d) ? d : [];
};

const loadProject = async (p: Project, comments: DomainEvent[]) => {
  const [review, runs, coverage, page] = await Promise.allSettled([
    reviewAPI.get(p.id),
    vvAPI.listRuns(p.id),
    vvAPI.coverage(p.id),
    artifactAPI.listPage(p.id, { limit: 1 }),
  ]);
  const rawCov = settled(coverage);
  const cov = rawCov && Array.isArray(rawCov.entries) ? rawCov : rawCov ? { ...rawCov, entries: [] } : null;
  const summary = cov?.summary;
  const open = list(runs).filter((r) => r.status === 'in-progress').slice(0, 3);
  const failingAny = (cov?.entries || []).some((e) => Object.values(e.latest_results || {}).includes('fail'));
  const raw = settled(review);
  const queue = raw && Array.isArray(raw.suspect_links) && Array.isArray(raw.in_review_artifacts) ? raw : null;
  const needsNames = failingAny || comments.length > 0 || open.length > 0 || (queue?.suspect_links.length || 0) > 0;
  const [artifacts, runResults] = await Promise.all([
    needsNames ? artifactAPI.list(p.id).then((r) => (Array.isArray(r.data) ? r.data : [])).catch(() => [] as Artifact[]) : Promise.resolve([] as Artifact[]),
    Promise.all(open.map(async (run) => { const d = (await vvAPI.listResults(run.id).catch(() => null))?.data; return { run, results: Array.isArray(d) ? d : [] }; })),
  ]);
  const total = page.status === 'fulfilled' ? parseInt(page.value.headers?.['x-total-count'] ?? '', 10) : NaN;
  const todo = buildTodo({
    projectId: p.id,
    review: queue,
    runs: runResults,
    testCases: artifacts.filter((a) => a.type === 'test-case').length,
    coverage: cov,
    comments,
    byId: new Map(artifacts.map((a) => [a.id, a])),
  }).map((t) => ({ ...t, project: p.name }));
  return {
    todo,
    summary,
    figures: {
      count: Number.isFinite(total) ? total : null,
      traced: percent(summary?.covered, summary?.total),
      verified: percent(summary?.verified, summary?.total),
      failing: summary ? summary.fail || 0 : null,
    },
  };
};

export const loadWorkspaceHome = async (projects: Project[]): Promise<WorkspaceHomeData> => {
  const names = new Map(projects.map((p) => [p.id, p.name]));
  const [events, chatter, proposals] = await Promise.allSettled([
    eventsAPI.list({ limit: 12 }),
    eventsAPI.list({ event_type: 'chatter.created', limit: 30 }),
    proposalsAPI.list({ status: 'pending' }),
  ]);
  const comments = list(chatter).filter((c) => c.payload?.entry_type === 'comment');
  const out: WorkspaceHomeData = {
    todo: [],
    activity: list(events)
      .filter((e) => !e.project_id || names.has(e.project_id))
      .map((e) => ({ ...describeEvent(e), project: names.get(e.project_id || '') || '' })),
    figures: {},
    proposals: list(proposals).filter((p) => !p.project_id || names.has(p.project_id)),
    totals: { artifacts: null, traced: null, verified: null, failing: 0 },
  };
  let artifacts = 0;
  let counted = false;
  let covered = 0;
  let verified = 0;
  let reqs = 0;
  for (let i = 0; i < projects.length; i += BATCH) {
    const batch = projects.slice(i, i + BATCH);
    const loaded = await Promise.all(
      batch.map((p) => loadProject(p, comments.filter((c) => c.project_id === p.id).slice(0, 3))),
    );
    batch.forEach((p, n) => {
      const l = loaded[n];
      out.todo.push(...l.todo);
      out.figures[p.id] = l.figures;
      if (l.figures.count !== null) {
        artifacts += l.figures.count;
        counted = true;
      }
      if (l.summary?.total) {
        reqs += l.summary.total;
        covered += l.summary.covered || 0;
        verified += l.summary.verified || 0;
      }
      out.totals.failing += l.figures.failing || 0;
    });
  }
  out.totals.artifacts = counted ? artifacts : null;
  out.totals.traced = percent(covered, reqs);
  out.totals.verified = percent(verified, reqs);
  return out;
};

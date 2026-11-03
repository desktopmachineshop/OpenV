// What the project Home page shows, and where each part comes from. Every
// figure is read from an endpoint the app already calls elsewhere; nothing is
// stored for the page. Each read is settled on its own, so one that fails
// (a viewer without a right, a slow endpoint) leaves its card empty rather
// than the page.
import {
  artifactAPI,
  baselineAPI,
  eventsAPI,
  membersAPI,
  projectAPI,
  proposalsAPI,
  reviewAPI,
  vvAPI,
  type Artifact,
  type Baseline,
  type CoverageReport,
  type DomainEvent,
  type Project,
  type Proposal,
  type ReviewQueue,
  type TestResult,
  type TestRun,
} from '../api/client';

export type TodoKind = 'approve' | 'suspect' | 'test run' | 'failing' | 'comment';

export interface TodoItem {
  key: string;
  kind: TodoKind;
  ref: string;
  title: string;
  meta: string;
  action: string;
  to: string;
}

export type ActivityTone = 'ok' | 'bad' | 'warn' | 'agent' | 'info';

export interface ActivityItem {
  id: string;
  who: string;
  what: string;
  ref: string;
  when: string;
  tone: ActivityTone;
}

export interface ProjectCard {
  id: string;
  name: string;
  count: number | null;
  traced: number | null;
  verified: number | null;
}

export interface HomeData {
  project: Project | null;
  artifacts: number | null;
  traced: number | null;
  verified: number | null;
  failing: number | null;
  suspect: number | null;
  lastBaseline: Baseline | null;
  members: number | null;
  todo: TodoItem[];
  activity: ActivityItem[];
  projects: ProjectCard[];
  proposals: Proposal[];
}

const settled = <T>(r: PromiseSettledResult<{ data: T }>): T | null => (r.status === 'fulfilled' ? r.value.data : null);

export const percent = (part: number | undefined, total: number | undefined): number | null =>
  total ? Math.round(((part || 0) / total) * 100) : null;

/** "10:42" today, "Yesterday 17:20", or "12 Oct" before that. */
export const whenLabel = (iso: string, now: Date = new Date()): string => {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '';
  const time = t.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((day(now) - day(t)) / 86400000);
  if (days === 0) return time;
  if (days === 1) return `Yesterday ${time}`;
  return t.toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
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

const VERBS: Record<string, string> = {
  'artifact.created': 'created',
  'artifact.updated': 'edited',
  'artifact.deleted': 'deleted',
  'artifact.restored': 'restored',
  'link.created': 'linked',
  'link.updated': 'changed a link of',
  'link.deleted': 'removed a link of',
  'baseline.captured': 'captured baseline',
  'baseline.deleted': 'deleted baseline',
  'project.review_round_started': 'started a review round',
  'chatter.created': 'commented on',
  'workitem.created': 'added to the board',
  'workitem.moved': 'moved on the board',
  'workitem.updated': 'updated on the board',
  'agentrun.finished': 'finished a run',
  'agentrun.successors_skipped': 'skipped the next steps of a run',
  'proposal.created': 'proposed a change to',
  'project.member_added': 'added a member',
  'project.member_role_changed': "changed a member's role",
  'project.member_removed': 'removed a member',
};

/** One line of the audit trail: who did what to which thing, and its dot. */
export const describeEvent = (e: DomainEvent, now?: Date): ActivityItem => {
  const p = e.payload || {};
  const agent = e.actor_kind === 'agent';
  let what = VERBS[e.event_type] || e.event_type;
  let tone: ActivityTone = agent ? 'agent' : 'info';
  if (e.event_type === 'artifact.status_changed') {
    what = p.to === 'approved' ? 'approved' : p.to === 'in_review' ? 'sent for review' : `moved to ${String(p.to || '').replace('_', ' ')}`;
    if (p.to === 'approved') tone = 'ok';
  } else if (e.event_type === 'testrun.recorded') {
    what = `recorded ${p.status || 'a result'} on`;
    tone = p.status === 'fail' ? 'bad' : p.status === 'pass' ? 'ok' : p.status === 'blocked' ? 'warn' : tone;
  } else if (e.event_type === 'agentrun.finished') {
    tone = p.status === 'succeeded' ? 'agent' : 'bad';
  } else if (e.event_type === 'artifact.deleted' || e.event_type === 'link.deleted') {
    tone = 'warn';
  } else if (e.event_type.startsWith('proposal.')) {
    tone = 'agent';
  }
  const ref = e.entity_name || p.title || p.name || '';
  return {
    id: e.id,
    who: e.actor_name || (agent ? 'Agent' : e.actor === 'system' ? 'System' : 'Someone'),
    what,
    ref: e.event_type === 'project.review_round_started' || e.event_type.startsWith('project.member') ? '' : ref,
    when: whenLabel(e.created_at, now),
    tone,
  };
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
}

const refOf = (byId: Map<string, Artifact>, id: string): string => byId.get(id)?.ref || '';

/** What waits on the team, in the order the design lists it. */
export const buildTodo = ({ projectId, review, runs, testCases, coverage, comments, byId, now }: TodoSources): TodoItem[] => {
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
      action: 'Confirm',
      to: `${base}/review`,
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
      action: 'Reply',
      to: open(id),
    });
  }
  return items;
};

/** The workspace's projects with their size and V&V figures, the current one first. */
const loadProjects = async (currentId: string, current: ProjectCard): Promise<ProjectCard[]> => {
  const list = (await projectAPI.list().catch(() => null))?.data || [];
  const others = list.filter((p) => p.id !== currentId).slice(0, 4);
  const cards = await Promise.all(
    others.map(async (p): Promise<ProjectCard> => {
      const [cov, page] = await Promise.allSettled([vvAPI.coverage(p.id), artifactAPI.listPage(p.id, { limit: 1 })]);
      const summary = cov.status === 'fulfilled' ? cov.value.data.summary : undefined;
      const total = page.status === 'fulfilled' ? parseInt(page.value.headers?.['x-total-count'] ?? '', 10) : NaN;
      return {
        id: p.id,
        name: p.name,
        count: Number.isFinite(total) ? total : null,
        traced: percent(summary?.covered, summary?.total),
        verified: percent(summary?.verified, summary?.total),
      };
    })
  );
  return [current, ...cards];
};

export const loadHome = async (projectId: string): Promise<HomeData> => {
  const [project, artifacts, coverage, baselines, members, review, runs, events, comments, proposals] =
    await Promise.allSettled([
      projectAPI.get(projectId),
      artifactAPI.list(projectId),
      vvAPI.coverage(projectId),
      baselineAPI.list(projectId),
      membersAPI.list(projectId),
      reviewAPI.get(projectId),
      vvAPI.listRuns(projectId),
      eventsAPI.list({ project_id: projectId, limit: 8 }),
      eventsAPI.list({ project_id: projectId, event_type: 'chatter.created', limit: 10 }),
      proposalsAPI.list({ project_id: projectId, status: 'pending' }),
    ]);
  const all = settled(artifacts) || [];
  const byId = new Map(all.map((a) => [a.id, a]));
  const cov = settled(coverage);
  const summary = cov?.summary;
  const runList = settled(runs) || [];
  const open = runList.filter((r) => r.status === 'in-progress').slice(0, 3);
  const runResults = await Promise.all(
    open.map(async (run) => ({ run, results: (await vvAPI.listResults(run.id).catch(() => null))?.data || [] }))
  );
  const reviewQueue = settled(review);
  const testCases = all.filter((a) => a.type === 'test-case').length;
  const list = settled(baselines) || [];
  const lastBaseline = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0] || null;
  const proj = settled(project);
  const current: ProjectCard = {
    id: projectId,
    name: proj?.name || 'This project',
    count: artifacts.status === 'fulfilled' ? all.length : null,
    traced: percent(summary?.covered, summary?.total),
    verified: percent(summary?.verified, summary?.total),
  };
  const commentEvents = (settled(comments) || []).filter((c) => c.payload?.entry_type === 'comment').slice(0, 3);
  return {
    project: proj,
    artifacts: current.count,
    traced: current.traced,
    verified: current.verified,
    failing: summary ? summary.fail || 0 : null,
    suspect: reviewQueue ? reviewQueue.suspect_links.length : null,
    lastBaseline,
    members: settled(members)?.length ?? null,
    todo: buildTodo({ projectId, review: reviewQueue, runs: runResults, testCases, coverage: cov, comments: commentEvents, byId }),
    activity: (settled(events) || []).map((e) => describeEvent(e)),
    projects: await loadProjects(projectId, current),
    proposals: settled(proposals) || [],
  };
};

/** A pending proposal in a few words: "create a test case", "update REQ-4". */
export const proposalLabel = (p: Proposal, byId?: Map<string, Artifact>): string => {
  const op = (p.op || 'change').replace(/_/g, ' ');
  const title = p.payload?.title ? ` “${p.payload.title}”` : '';
  const target = p.target_id ? byId?.get(p.target_id)?.ref || '' : '';
  return `${op}${target ? ` ${target}` : ''}${title}`;
};

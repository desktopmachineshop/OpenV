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
  type Project,
  type Proposal,
} from '../api/client';
import {
  buildTodo,
  describeEvent,
  percent,
  type ActivityItem,
  type TodoItem,
} from '../components/ProjectListHomeData';

export {
  agoLabel,
  buildTodo,
  describeEvent,
  percent,
  whenLabel,
  type ActivityItem,
  type ActivityTone,
  type TodoItem,
  type TodoKind,
} from '../components/ProjectListHomeData';

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

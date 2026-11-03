// What needs a member and what happened, built from reads the app already
// makes: the review queue, the open test runs, coverage and comments for the
// needs list, and domain events for the audit trail. The project Home
// (views/ProjectHomeData.ts) builds them for one project; the workspace Home
// on the Projects page (ProjectListHome) builds them for every project the
// member can see.
import type { Artifact, CoverageReport, DomainEvent, ReviewQueue, TestResult, TestRun } from '../api/client';

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


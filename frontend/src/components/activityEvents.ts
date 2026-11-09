// What a domain event says, in words a workspace member reads at a glance,
// and where it leads. One module, so the workspace Home's Activity panel, the
// project Summary and the project's Activity log always say the same thing
// about the same event (#734) and open the same page from it (#718).
import type { DomainEvent } from '../api/client';

export type ActivityTone = 'ok' | 'bad' | 'warn' | 'agent' | 'info';

export interface ActivityItem {
  id: string;
  who: string;
  what: string;
  ref: string;
  when: string;
  tone: ActivityTone;
  /** The page the event is about, or '' when there is nowhere to go (a deleted artifact). */
  to: string;
}

/** The dot (or chip colour) of each tone, as the Home draws it. */
export const TONE_COLOR: Record<ActivityTone, string> = {
  ok: 'var(--success)',
  bad: 'var(--danger)',
  warn: 'var(--warning)',
  agent: 'var(--purple)',
  info: 'var(--accent)',
};

/** "10:42" today, "Yesterday 17:20", or "12 Oct" before that. */
export const whenLabel = (iso: string, now: Date = new Date()): string => {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '';
  const time = t.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  const days = daysBefore(t, now);
  if (days === 0) return time;
  if (days === 1) return `Yesterday ${time}`;
  return t.toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
};

const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
const daysBefore = (t: Date, now: Date) => Math.round((startOfDay(now) - startOfDay(t)) / 86400000);

/** The day header an event sits under in the log: "Today", "Yesterday", "3 Nov" (with the year when not this one). */
export const dayLabel = (iso: string, now: Date = new Date()): string => {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '';
  const days = daysBefore(t, now);
  if (days === 0) return 'Today';
  if (days === 1) return 'Yesterday';
  return t.toLocaleDateString(undefined, {
    day: 'numeric',
    month: 'short',
    ...(t.getFullYear() === now.getFullYear() ? {} : { year: 'numeric' }),
  });
};

export const VERBS: Record<string, string> = {
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

/** What each kind of event is called, for the log's type filter. */
export const EVENT_TYPE_LABELS: Record<string, string> = {
  'artifact.created': 'Artifact created',
  'artifact.updated': 'Artifact edited',
  'artifact.deleted': 'Artifact deleted',
  'artifact.status_changed': 'Status changed',
  'artifact.restored': 'Version restored',
  'link.created': 'Link created',
  'link.updated': 'Link changed',
  'link.deleted': 'Link removed',
  'baseline.captured': 'Baseline captured',
  'baseline.deleted': 'Baseline deleted',
  'project.review_round_started': 'Review round started',
  'chatter.created': 'Comment or note',
  'testrun.recorded': 'Test result recorded',
  'workitem.created': 'Work item added',
  'workitem.moved': 'Work item moved',
  'workitem.updated': 'Work item updated',
  'agentrun.finished': 'Agent run finished',
  'agentrun.successors_skipped': 'Crew steps skipped',
  'proposal.created': 'Agent proposal',
  'project.member_added': 'Member added',
  'project.member_role_changed': 'Member role changed',
  'project.member_removed': 'Member removed',
};

/** A status, board column or other stored word as a reader writes it: "in_review" → "in review". */
export const humanWord = (raw: unknown): string => String(raw ?? '').replace(/[_-]+/g, ' ').trim();

const str = (v: unknown): string => (typeof v === 'string' ? v : '');

/**
 * The page an event is about (#718), or '' when there is none: the artifact
 * opened in its project, a link's source artifact, the board, the run, the
 * test run, the baseline compared, or the artifact with its notes open for a
 * comment. A deleted artifact opens from its history and a removed link opens
 * its source as it was before; an event with no project leads nowhere.
 */
export const eventTarget = (e: DomainEvent): string => {
  if (!e.project_id) return '';
  const base = `/projects/${e.project_id}`;
  const p = e.payload || {};
  const artifact = (id: string, notes = false) =>
    id ? `${base}/requirements?artifact=${encodeURIComponent(id)}${notes ? '&notes=1' : ''}` : '';
  const family = e.event_type.split('.')[0];
  switch (e.event_type) {
    // A deleted artifact opens from its history, which the soft delete keeps.
    case 'artifact.deleted':
      return artifact(e.entity_id || '');
    // A removed link opens its source as it was while it still had the link.
    case 'link.deleted':
      return str(p.from_id) && e.entity_id
        ? `${artifact(str(p.from_id))}&link=${encodeURIComponent(e.entity_id)}&at=${encodeURIComponent(e.created_at)}`
        : artifact(str(p.from_id));
    case 'baseline.captured':
      return e.entity_id ? `${base}/baselines/${e.entity_id}/compare` : `${base}/baselines`;
    case 'baseline.deleted':
      return `${base}/baselines`;
    case 'project.review_round_started':
      return `${base}/review`;
    case 'chatter.created':
      return artifact(str(p.artifact_id), true);
    case 'testrun.recorded':
      return str(p.run_id) ? `${base}/vv/runs/${str(p.run_id)}` : `${base}/vv`;
    case 'proposal.created':
      return `${base}/agent-runs${str(p.run_id) ? `?run=${str(p.run_id)}` : ''}`;
  }
  if (family === 'artifact') return artifact(e.entity_id || '');
  if (family === 'link') return artifact(str(p.from_id));
  if (family === 'workitem') return `${base}/board`;
  if (family === 'agentrun') return `${base}/agent-runs${e.entity_id ? `?run=${e.entity_id}` : ''}`;
  if (e.event_type.startsWith('project.member')) return `${base}/settings?tab=members`;
  return '';
};

/** What the event did, in a few words: "approved", "moved to in review", "recorded fail on". */
export const verbOf = (e: DomainEvent): string => {
  const p = e.payload || {};
  if (e.event_type === 'artifact.status_changed') {
    return p.to === 'approved' ? 'approved' : p.to === 'in_review' ? 'sent for review' : `moved to ${humanWord(p.to)}`;
  }
  if (e.event_type === 'testrun.recorded') return `recorded ${p.status || 'a result'} on`;
  return VERBS[e.event_type] || humanWord(e.event_type.replace('.', ' '));
};

/** The tone of an event's dot: green for an approval or a pass, red for a failure, purple for agent work. */
export const toneOf = (e: DomainEvent): ActivityTone => {
  const p = e.payload || {};
  const agent = e.actor_kind === 'agent';
  if (e.event_type === 'artifact.status_changed') return p.to === 'approved' ? 'ok' : agent ? 'agent' : 'info';
  if (e.event_type === 'testrun.recorded') {
    if (p.status === 'fail') return 'bad';
    if (p.status === 'pass') return 'ok';
    if (p.status === 'blocked') return 'warn';
  }
  if (e.event_type === 'agentrun.finished') return p.status === 'succeeded' ? 'agent' : 'bad';
  if (e.event_type === 'artifact.deleted' || e.event_type === 'link.deleted') return 'warn';
  if (e.event_type.startsWith('proposal.')) return 'agent';
  return agent ? 'agent' : 'info';
};

/** Who did it, by name: the server's name for the actor, or what kind of actor it was. */
export const whoOf = (e: DomainEvent): string =>
  e.actor_name ||
  (e.actor_kind === 'agent'
    ? 'Agent'
    : e.actor_kind === 'worker'
      ? 'Workspace runner'
      : e.actor === 'system' || e.actor_kind === 'system' || !e.actor
        ? 'System'
        : 'Someone');

/** Whether the event names a thing a reader would look for (not a review round or a membership change). */
export const namesThing = (e: DomainEvent): boolean =>
  e.event_type !== 'project.review_round_started' && !e.event_type.startsWith('project.member');

/** One line of the audit trail: who did what to which thing, its dot and where it leads. */
export const describeEvent = (e: DomainEvent, now?: Date): ActivityItem => {
  const p = e.payload || {};
  const ref = e.entity_name || p.title || p.name || '';
  return {
    id: e.id,
    who: whoOf(e),
    what: verbOf(e),
    ref: namesThing(e) ? ref : '',
    when: whenLabel(e.created_at, now),
    tone: toneOf(e),
    to: eventTarget(e),
  };
};

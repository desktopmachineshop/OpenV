import { ARTIFACT_STATUSES, LINK_TYPE_RULES, type FeatureKey } from '../../generated/contract';
import { text } from './artifactTemplates';

/**
 * The rest of a project's API as V&V Assistant cards (assistant-full-scope):
 * releases and what artifacts target, links, review status, deleting,
 * comments, to-dos, board tasks, baselines, a review round and test runs.
 *
 * Beside the artifact, edit and move cards (suggestionDrafts.ts), these are
 * the other changes a member makes to a project. Each is still a card: the
 * assistant proposes it and the member's click applies it, through the same
 * calls the app's own pages make (projectActionsApply.ts), so the assistant
 * writes nothing itself and can do no more than the member could.
 *
 * This module is the pure half: what a card means, or why it means nothing
 * yet, and what it says on its face. The shapes are the ones the server's
 * prompt teaches (internal/api/guided_copilot_actions.go).
 */

/** Feature gate for these cards (REQ-137). */
export const ASSISTANT_FULL_SCOPE_FEATURE: FeatureKey = 'assistant-full-scope';

export const PROJECT_ACTION_KINDS: ReadonlySet<string> = new Set([
  'release',
  'target',
  'link',
  'status',
  'delete',
  'comment',
  'todo',
  'task',
  'baseline',
  'review',
  'test_run',
]);
/** Each card's heading. A kind the chat panel has no label for is shown read-only. */
export const PROJECT_ACTION_LABELS: Record<string, string> = {
  release: 'Release',
  target: 'Release target',
  link: 'Link',
  status: 'Review status',
  delete: 'Delete',
  comment: 'Comment',
  todo: 'To-do',
  task: 'Board task',
  baseline: 'Baseline',
  review: 'Review round',
  test_run: 'Test run',
};
/**
 * Cards Apply all leaves out: deleting is done one card at a time, by the
 * card's own button, never as a side effect of applying a reply's batch.
 */
export const ONE_AT_A_TIME_KINDS: ReadonlySet<string> = new Set(['delete']);
export const isProjectActionKind = (kind: unknown): boolean => PROJECT_ACTION_KINDS.has(String(kind));
export const FULL_SCOPE_GATED_REASON =
  'Releases, links, status and the assistant’s other project changes reach this workspace with its next stable release.';

export const RELEASE_STATUSES = ['planned', 'in_progress', 'released'] as const;
export const RESULT_STATUSES = ['pass', 'fail', 'blocked', 'not-run'] as const;
const LINK_TYPES: readonly string[] = LINK_TYPE_RULES.map((r) => r.type);
const STATUSES: readonly string[] = ARTIFACT_STATUSES.map((s) => s.value);
const DATE = /^\d{4}-\d{2}-\d{2}$/;

export type ProjectAction =
  | {
      kind: 'release';
      /** The release to change, by name or id; absent to add a new one. */
      release?: string;
      fields: { name?: string; description?: string; target_date?: string; status?: string };
    }
  | { kind: 'target'; refs: string[]; release?: string; until?: string; clear: boolean }
  | { kind: 'link'; action: 'add' | 'remove' | 'confirm'; from: string; to: string; type?: string }
  | { kind: 'status'; refs: string[]; status: string }
  | { kind: 'delete'; refs: string[] }
  | { kind: 'comment'; ref: string; message: string }
  | { kind: 'todo'; text: string; assignee?: string; due?: string; ref?: string }
  | { kind: 'task'; title: string; description: string; refs: string[]; assignee?: string; due?: string }
  | { kind: 'baseline'; name: string }
  | { kind: 'review'; types?: string[] }
  | {
      kind: 'test_run';
      name: string;
      description: string;
      results: { ref: string; status: string; notes: string }[];
      complete: boolean;
    };

export type ProjectActionPlan = { outcome: 'action'; action: ProjectAction } | { outcome: 'refused'; reason: string };

type Suggestion = { kind: string; [key: string]: any };

/** The artifacts a card names: "refs" as a list, or a single "ref". */
const refsOf = (s: Suggestion): string[] => {
  const list = Array.isArray(s.refs) ? s.refs : s.ref !== undefined ? [s.ref] : [];
  return list.map((r: unknown) => text(r)).filter(Boolean);
};

/** An optional date: absent, or YYYY-MM-DD. */
const dateOf = (value: unknown): string | undefined | null => {
  const d = text(value);
  if (!d) return undefined;
  return DATE.test(d) ? d : null;
};

const refuse = (reason: string): ProjectActionPlan => ({ outcome: 'refused', reason });
const act = (action: ProjectAction): ProjectActionPlan => ({ outcome: 'action', action });

/** What one of these cards means, or the reason a person can act on why not. */
export const planProjectAction = (s: Suggestion): ProjectActionPlan => {
  switch (s.kind) {
    case 'release': {
      const fields: { name?: string; description?: string; target_date?: string; status?: string } = {};
      if (s.name !== undefined) fields.name = text(s.name);
      if (s.description !== undefined) fields.description = String(s.description ?? '');
      if (s.target_date !== undefined) {
        const d = text(s.target_date);
        if (d && !DATE.test(d)) return refuse(`"${d}" is not a date; write it as YYYY-MM-DD.`);
        fields.target_date = d;
      }
      if (s.status !== undefined) {
        const status = text(s.status);
        if (!(RELEASE_STATUSES as readonly string[]).includes(status)) {
          return refuse(`"${status}" is not a release status; use planned, in_progress or released.`);
        }
        fields.status = status;
      }
      const existing = text(s.release);
      if (!existing && !fields.name) return refuse('A new release needs a name.');
      if (fields.name === '') return refuse('A release cannot have an empty name.');
      if (existing && Object.keys(fields).length === 0) return refuse(`The change to ${existing} changes nothing.`);
      return act({ kind: 'release', ...(existing ? { release: existing } : {}), fields });
    }

    case 'target': {
      const refs = refsOf(s);
      if (refs.length === 0) return refuse('The release target names no artifact.');
      const clear = s.clear === true;
      const release = text(s.release);
      if (!clear && !release) return refuse('The release target names no release.');
      const until = text(s.until);
      return act({ kind: 'target', refs, clear, ...(clear ? {} : { release }), ...(until && !clear ? { until } : {}) });
    }

    case 'link': {
      const action = text(s.action) || 'add';
      if (action !== 'add' && action !== 'remove' && action !== 'confirm') {
        return refuse(`"${action}" is not something a link card does; use add, remove or confirm.`);
      }
      const from = text(s.from);
      const to = text(s.to);
      if (!from || !to) return refuse('A link card needs both ends: from and to.');
      const type = text(s.type);
      if (type && !LINK_TYPES.includes(type)) return refuse(`"${type}" is not a link type.`);
      if (action === 'add' && !type) return refuse(`The link from ${from} to ${to} has no type.`);
      return act({ kind: 'link', action, from, to, ...(type ? { type } : {}) });
    }

    case 'status': {
      const refs = refsOf(s);
      if (refs.length === 0) return refuse('The status change names no artifact.');
      const status = text(s.status);
      if (!STATUSES.includes(status)) return refuse(`"${status}" is not a review status.`);
      return act({ kind: 'status', refs, status });
    }

    case 'delete': {
      const refs = refsOf(s);
      if (refs.length === 0) return refuse('The delete names no artifact.');
      return act({ kind: 'delete', refs });
    }

    case 'comment': {
      const [ref] = refsOf(s);
      const message = String(s.message ?? '').trim();
      if (!ref) return refuse('The comment names no artifact.');
      if (!message) return refuse(`The comment on ${ref} is empty.`);
      return act({ kind: 'comment', ref, message });
    }

    case 'todo': {
      const body = text(s.text);
      if (!body) return refuse('The to-do says nothing to do.');
      const due = dateOf(s.due);
      if (due === null) return refuse(`"${text(s.due)}" is not a date; write it as YYYY-MM-DD.`);
      const [ref] = refsOf(s);
      const assignee = text(s.assignee);
      return act({ kind: 'todo', text: body, ...(assignee ? { assignee } : {}), ...(due ? { due } : {}), ...(ref ? { ref } : {}) });
    }

    case 'task': {
      const title = text(s.title);
      if (!title) return refuse('The task has no title.');
      const due = dateOf(s.due);
      if (due === null) return refuse(`"${text(s.due)}" is not a date; write it as YYYY-MM-DD.`);
      const assignee = text(s.assignee);
      return act({
        kind: 'task',
        title,
        description: String(s.description ?? ''),
        refs: refsOf(s),
        ...(assignee ? { assignee } : {}),
        ...(due ? { due } : {}),
      });
    }

    case 'baseline': {
      const name = text(s.name);
      if (!name) return refuse('The baseline has no name.');
      return act({ kind: 'baseline', name });
    }

    case 'review': {
      const types = Array.isArray(s.types) ? s.types.map((t: unknown) => text(t)).filter(Boolean) : [];
      return act({ kind: 'review', ...(types.length ? { types } : {}) });
    }

    case 'test_run': {
      const name = text(s.name);
      if (!name) return refuse('The test run has no name.');
      const raw = Array.isArray(s.results) ? s.results : [];
      const results: { ref: string; status: string; notes: string }[] = [];
      for (const r of raw) {
        const ref = text(r?.ref);
        const status = text(r?.status);
        if (!ref) return refuse('A result in the test run names no test case.');
        if (!(RESULT_STATUSES as readonly string[]).includes(status)) {
          return refuse(`"${status}" is not a result for ${ref}; use pass, fail, blocked or not-run.`);
        }
        results.push({ ref, status, notes: String(r?.notes ?? '') });
      }
      return act({ kind: 'test_run', name, description: String(s.description ?? ''), results, complete: s.complete === true });
    }

    default:
      return refuse(`I do not know how to apply a "${s.kind}".`);
  }
};

const list = (refs: string[]): string => (refs.length > 4 ? `${refs.slice(0, 4).join(', ')} and ${refs.length - 4} more` : refs.join(', '));

/** The card's heading and the line under it. */
export const projectActionSummary = (s: Suggestion): { title: string; detail: string } => {
  const plan = planProjectAction(s);
  if (plan.outcome === 'refused') return { title: String(s.kind), detail: plan.reason };
  const a = plan.action;
  switch (a.kind) {
    case 'release': {
      const parts = [
        a.fields.name !== undefined && a.release ? `name → ${a.fields.name}` : '',
        a.fields.target_date !== undefined ? (a.fields.target_date ? `target ${a.fields.target_date}` : 'no target date') : '',
        a.fields.status ? `status ${a.fields.status.replace('_', ' ')}` : '',
      ].filter(Boolean);
      return { title: a.release ? `Release ${a.release}` : `New release: ${a.fields.name}`, detail: parts.join(' · ') || a.fields.description || '' };
    }
    case 'target':
      return {
        title: list(a.refs),
        detail: a.clear ? 'Inherit the parent’s releases' : `In ${a.release}${a.until ? ` until ${a.until}` : ' onwards'}`,
      };
    case 'link':
      return {
        title: `${a.from} ${a.type || '→'} ${a.to}`,
        detail: a.action === 'remove' ? 'Remove this link' : a.action === 'confirm' ? 'Confirm this suspect link still holds' : 'Add this link',
      };
    case 'status':
      return { title: list(a.refs), detail: `Move to ${a.status.replace('_', ' ')}` };
    case 'delete':
      return { title: list(a.refs), detail: 'Delete' };
    case 'comment':
      return { title: `Comment on ${a.ref}`, detail: a.message };
    case 'todo':
      return { title: a.text, detail: [a.assignee, a.due ? `due ${a.due}` : '', a.ref].filter(Boolean).join(' · ') };
    case 'task':
      return { title: a.title, detail: [a.assignee, a.due ? `due ${a.due}` : '', a.refs.length ? list(a.refs) : ''].filter(Boolean).join(' · ') };
    case 'baseline':
      return { title: a.name, detail: 'A snapshot of the project as it stands' };
    case 'review':
      return { title: 'Start a review round', detail: a.types ? `Every draft ${a.types.join(', ')} goes to in review` : 'Every draft goes to in review' };
    case 'test_run':
      return {
        title: a.name,
        detail: `${a.results.length} result${a.results.length === 1 ? '' : 's'}${a.complete ? ', completed' : ''}`,
      };
  }
};

/** The card's button, and what it says once applied. */
export const projectActionLabels = (s: Suggestion): { button: string; done: string } => {
  switch (s.kind) {
    case 'release':
      return text(s.release) ? { button: 'Change release', done: '✓ Changed' } : { button: '+ Add release', done: '✓ Added' };
    case 'target':
      return { button: 'Set releases', done: '✓ Set' };
    case 'link': {
      const action = text(s.action) || 'add';
      if (action === 'remove') return { button: 'Remove link', done: '✓ Removed' };
      if (action === 'confirm') return { button: 'Confirm link', done: '✓ Confirmed' };
      return { button: '+ Add link', done: '✓ Linked' };
    }
    case 'status':
      return { button: 'Change status', done: '✓ Changed' };
    case 'delete':
      return { button: 'Delete', done: '✓ Deleted' };
    case 'comment':
      return { button: 'Post comment', done: '✓ Posted' };
    case 'todo':
      return { button: '+ Add to-do', done: '✓ Added' };
    case 'task':
      return { button: '+ Add to board', done: '✓ Added' };
    case 'baseline':
      return { button: 'Capture baseline', done: '✓ Captured' };
    case 'review':
      return { button: 'Start review', done: '✓ Started' };
    case 'test_run':
      return { button: 'Record run', done: '✓ Recorded' };
    default:
      return { button: 'Apply', done: '✓ Applied' };
  }
};

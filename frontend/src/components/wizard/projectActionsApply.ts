import {
  Artifact,
  artifactAPI,
  baselineAPI,
  chatterAPI,
  linkAPI,
  membersAPI,
  releasesAPI,
  reviewAPI,
  todosAPI,
  vvAPI,
  workItemsAPI,
  type ProjectMember,
  type TargetRelease,
} from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import type { ProjectAction } from './projectActions';
import { applyPlanAction, scheduleNewTask } from './planActionsApply';

/**
 * Applying a project-action card (projectActions.ts) with the calls the
 * app's own pages make for the same change: the Releases page's, the link
 * panel's, the review queue's, the to-do list's, the board's, the
 * Baselines page's and the test run view's. Nothing here goes around the
 * API's own checks — a member without the right to approve gets the API's
 * refusal on the card, exactly as on the page.
 */

/** What an apply needs: the project, and its artifacts as a working copy. */
export interface ActionContext {
  projectId: string;
  artifacts: Artifact[];
}

/** The artifact a card names, by reference (any case) or id. */
const find = (ctx: ActionContext, name: string): Artifact | null => {
  const upper = name.trim().toUpperCase();
  const lower = name.trim().toLowerCase();
  return ctx.artifacts.find((a) => (a.ref || '').toUpperCase() === upper || a.id === lower || a.id === name.trim()) || null;
};

const need = (ctx: ActionContext, name: string): Artifact => {
  const a = find(ctx, name);
  if (!a) throw new Error(`There is no artifact ${name} in this project.`);
  return a;
};

/** A release by its id or its name (any case), read fresh: an earlier card may have added it. */
const releaseNamed = async (projectId: string, name: string): Promise<TargetRelease> => {
  const res = await releasesAPI.list(projectId);
  const releases = Array.isArray(res.data) ? res.data : [];
  const wanted = name.trim().toLowerCase();
  const r = releases.find((x) => x.id === name.trim() || (x.name || '').trim().toLowerCase() === wanted);
  if (!r) {
    const names = releases.map((x) => x.name).join(', ');
    throw new Error(`There is no release ${name} in this project${names ? ` (it has ${names})` : ''}.`);
  }
  return r;
};

/** A project member by name or email (any case). */
const memberNamed = async (projectId: string, name: string): Promise<string> => {
  const res = await membersAPI.list(projectId);
  const members: ProjectMember[] = Array.isArray(res.data) ? res.data : [];
  const wanted = name.trim().toLowerCase().replace(/^@/, '');
  const m = members.find(
    (x) => (x.user_email || '').toLowerCase() === wanted || (x.user_name || '').toLowerCase() === wanted
  );
  if (!m) throw new Error(`${name} is not a member of this project.`);
  return m.user_id;
};

const due = (date?: string): string | null => (date ? `${date}T00:00:00Z` : null);

/**
 * Run one write per artifact a card names, carrying on past a failure so one
 * refused artifact does not stop the rest, and say which ones failed.
 */
const each = async (refs: string[], write: (ref: string) => Promise<void>): Promise<void> => {
  const failed: string[] = [];
  for (const ref of refs) {
    try {
      await write(ref);
    } catch (err) {
      failed.push(`${ref}: ${apiErrorMessage(err, 'not changed')}`);
    }
  }
  if (failed.length === refs.length && refs.length === 1) throw new Error(failed[0].replace(/^[^:]+: /, ''));
  if (failed.length) {
    const done = refs.length - failed.length;
    throw new Error(`${done ? `Applied to ${done} of ${refs.length}. ` : ''}${failed.join('; ')}`);
  }
};

/** Apply one card; throws with the reason when it cannot. */
export const applyProjectAction = async (ctx: ActionContext, a: ProjectAction): Promise<void> => {
  const { projectId } = ctx;
  switch (a.kind) {
    case 'release': {
      if (a.release) {
        const r = await releaseNamed(projectId, a.release);
        await releasesAPI.update(projectId, r.id, a.fields as any);
      } else {
        await releasesAPI.create(projectId, a.fields as any);
      }
      return;
    }

    case 'target': {
      // Resolved once before any write, so an unknown release changes nothing.
      const from = a.clear || !a.release ? null : await releaseNamed(projectId, a.release);
      const until = a.clear || !a.until ? null : await releaseNamed(projectId, a.until);
      await each(a.refs, async (ref) => {
        const target = need(ctx, ref);
        // The API replaces the attributes map whole: keep the others.
        const attributes: Record<string, any> = { ...(target.attributes || {}) };
        delete attributes.target_release;
        delete attributes.target_release_until;
        if (from) attributes.target_release = from.id;
        if (until) attributes.target_release_until = until.id;
        const res = await artifactAPI.update(target.id, { attributes });
        ctx.artifacts = ctx.artifacts.map((x) => (x.id === target.id ? res.data : x));
      });
      return;
    }

    case 'link': {
      const from = need(ctx, a.from);
      const to = need(ctx, a.to);
      if (a.action === 'add') {
        await linkAPI.create({ from_id: from.id, to_id: to.id, type: a.type });
        return;
      }
      const res = await linkAPI.listForArtifact(from.id);
      const links = (Array.isArray(res.data) ? res.data : []).filter(
        (l) => l.from_id === from.id && l.to_id === to.id && (!a.type || l.type === a.type)
      );
      if (links.length === 0) throw new Error(`There is no ${a.type ? `${a.type} ` : ''}link from ${a.from} to ${a.to}.`);
      for (const l of links) {
        if (a.action === 'remove') await linkAPI.delete(l.id);
        else await linkAPI.confirm(l.id);
      }
      return;
    }

    case 'status':
      await each(a.refs, async (ref) => {
        const target = need(ctx, ref);
        await artifactAPI.changeStatus(target.id, a.status as any);
      });
      return;

    case 'archive':
      await each(a.refs, async (ref) => {
        const target = need(ctx, ref);
        await artifactAPI.delete(target.id);
        ctx.artifacts = ctx.artifacts.filter((x) => x.id !== target.id);
      });
      return;

    case 'comment':
      await chatterAPI.create({ artifact_id: need(ctx, a.ref).id, message: a.message });
      return;

    case 'todo': {
      const source = a.ref ? need(ctx, a.ref) : null;
      await todosAPI.createProject(projectId, {
        text: a.text,
        assignee_id: a.assignee ? await memberNamed(projectId, a.assignee) : null,
        due_date: due(a.due),
        source_artifact_id: source ? source.id : null,
      });
      return;
    }

    case 'task': {
      const artifactIds = a.refs.map((ref) => need(ctx, ref).id);
      const created = await workItemsAPI.create(projectId, {
        title: a.title,
        description: a.description,
        artifact_ids: artifactIds,
        ...(a.assignee ? { assignee_type: 'user' as const, assignee_id: await memberNamed(projectId, a.assignee) } : {}),
        ...(a.due ? { due_date: due(a.due) } : {}),
      });
      await scheduleNewTask(projectId, created.data.id, a.plan);
      return;
    }

    case 'baseline':
      await baselineAPI.create(projectId, a.name);
      return;

    case 'review':
      await reviewAPI.startRound(projectId, a.types);
      return;

    case 'test_run': {
      // Every test case is resolved before the run exists, so a misnamed one
      // does not leave a half-recorded run behind.
      const results = a.results.map((r) => ({ ...r, id: need(ctx, r.ref).id }));
      const run = (await vvAPI.createRun(projectId, { name: a.name, ...(a.description ? { description: a.description } : {}) })).data;
      for (const r of results) {
        await vvAPI.upsertResult(run.id, { test_case_id: r.id, status: r.status, ...(r.notes ? { notes: r.notes } : {}) });
      }
      if (a.complete) await vvAPI.updateRun(run.id, 'completed');
      return;
    }

    default:
      await applyPlanAction(projectId, a);
  }
};

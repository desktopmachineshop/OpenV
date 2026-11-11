import { planAPI, todosAPI, workItemsAPI, type PlanScheduleInput, type PlanTask, type ProjectPlan } from '../../api/client';
import type { PlanAction, ScheduleFields } from './planActions';

/**
 * Applying a plan card (planActions.ts) with the calls the Plan page makes
 * for the same change. The plan is read fresh for each card, so a task or a
 * milestone an earlier card of the same reply added can be named.
 */

const same = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase();

/** A task by its id, its outline number or its title (any case). */
export const taskNamed = (plan: ProjectPlan, name: string): PlanTask => {
  const wanted = name.trim();
  const byId = plan.tasks.find((t) => t.id === wanted || t.wbs === wanted);
  if (byId) return byId;
  const titled = plan.tasks.filter((t) => same(t.title, wanted));
  if (titled.length === 1) return titled[0];
  if (titled.length > 1) throw new Error(`${titled.length} tasks are called ${name}; name it by its outline number (${titled.map((t) => t.wbs).join(' or ')}).`);
  throw new Error(`There is no task ${name} on this project's board.`);
};

/** A milestone's key, by its key or its name (any case). */
const milestoneNamed = (plan: ProjectPlan, name: string) => {
  const m = plan.milestones.find((x) => x.key === name.trim() || same(x.name, name));
  if (!m) {
    const names = plan.milestones.map((x) => x.name).join(', ');
    throw new Error(`There is no milestone ${name} in this project${names ? ` (it has ${names})` : ''}.`);
  }
  return m;
};

/** The schedule route's body for a card's fields, with every name resolved before anything is written. */
export const scheduleInput = (plan: ProjectPlan, f: ScheduleFields): PlanScheduleInput => {
  const out: PlanScheduleInput = {};
  if (f.start !== undefined) out.start_date = f.start;
  if (f.due !== undefined) out.due_date = f.due;
  if (f.duration_days !== undefined) out.duration_days = f.duration_days;
  if (f.effort_days !== undefined) out.effort_days = f.effort_days;
  if (f.percent_complete !== undefined) out.percent_complete = f.percent_complete;
  if (f.deadline !== undefined) out.deadline = f.deadline;
  if (f.parent !== undefined) out.parent_id = f.parent ? taskNamed(plan, f.parent).id : '';
  if (f.milestone !== undefined) out.milestone = f.milestone ? milestoneNamed(plan, f.milestone).key : '';
  if (f.after !== undefined) out.relationships = f.after.map((a) => ({ task_id: taskNamed(plan, a.task).id, type: a.type, lag_days: a.lag_days, lag_unit: 'd' as const }));
  return out;
};

/** Places a task an earlier call added: the plan fields of a board-task card. */
export const scheduleNewTask = async (projectId: string, workItemId: string, f: ScheduleFields): Promise<void> => {
  if (Object.keys(f).length === 0) return;
  const plan = (await planAPI.get(projectId)).data;
  await planAPI.schedule(workItemId, scheduleInput(plan, f));
};

/** Apply one plan card; throws with the reason when it cannot. */
export const applyPlanAction = async (projectId: string, a: PlanAction): Promise<void> => {
  if (a.kind === 'plan') {
    if (a.action === 'recalculate') await planAPI.recalculate(projectId);
    else if (a.action === 'level') await planAPI.level(projectId);
    else if (a.action === 'baseline') await planAPI.saveBaseline(projectId, a.name || '');
    else await planAPI.rescheduleRemaining(projectId);
    return;
  }
  const plan = (await planAPI.get(projectId)).data;
  switch (a.kind) {
    case 'plan_task': {
      const task = taskNamed(plan, a.task);
      const change = scheduleInput(plan, a.schedule);
      if (a.title) {
        // The board's update replaces the card: carry what it has.
        const { item } = (await workItemsAPI.get(task.id)).data;
        await workItemsAPI.update(task.id, { title: a.title, description: item.description, assignee_type: item.assignee_type, assignee_id: item.assignee_id, artifact_ids: item.artifact_ids, due_date: item.due_date });
      }
      if (Object.keys(change).length > 0) await planAPI.schedule(task.id, change);
      if (a.column && a.column !== task.column) await workItemsAPI.move(task.id, a.column, 0);
      return;
    }

    case 'milestone': {
      const known = new Set(plan.metric_catalogue.map((m) => m.key));
      const unknown = (a.fields.criteria || []).find((c) => !known.has(c.metric));
      if (unknown) throw new Error(`"${unknown.metric}" is not a metric a milestone can measure; use ${[...known].join(', ')}.`);
      const payload = {
        ...(a.fields.name !== undefined ? { name: a.fields.name } : {}),
        ...(a.fields.description !== undefined ? { description: a.fields.description } : {}),
        ...(a.fields.date !== undefined ? { target_date: a.fields.date } : {}),
        ...(a.fields.criteria !== undefined ? { criteria: a.fields.criteria } : {}),
      };
      if (!a.milestone) {
        await planAPI.createMilestone(projectId, payload);
        return;
      }
      const m = milestoneNamed(plan, a.milestone);
      if (m.kind === 'release') {
        // A release's milestone takes its name and date from the release.
        if (payload.name !== undefined || payload.target_date !== undefined || payload.description !== undefined) {
          throw new Error(`${m.name} is a release's milestone: change its name and date with a release card. Only its criteria are set here.`);
        }
        await planAPI.setReleaseCriteria(projectId, m.release_id!, payload.criteria || []);
        return;
      }
      await planAPI.updateMilestone(projectId, m.id, payload);
      return;
    }

    case 'task_todo':
      await planAPI.addTodo(taskNamed(plan, a.task).id, a.text, a.due);
      return;

    case 'todo_done': {
      const onTasks = plan.tasks.flatMap((t) => t.todos).filter((t) => same(t.text, a.text));
      const found = onTasks.length ? onTasks : ((await todosAPI.listProject(projectId)).data || []).filter((t) => same(t.text, a.text));
      if (found.length === 0) throw new Error(`There is no to-do "${a.text}" in this project.`);
      const todo = found.find((t) => t.done !== a.done) || found[0];
      await todosAPI.setDone(todo.id, a.done);
      return;
    }
  }
};

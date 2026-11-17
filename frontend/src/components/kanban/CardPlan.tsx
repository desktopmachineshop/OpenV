// What the plan holds on a card, beside the card opened in full on the
// board: the task's panel of the Gantt page (components/plan/TaskPanel.tsx),
// with its schedule, relationships, resources, milestone, to-dos and
// sub-tasks. A card is a task of the plan, so both pages open it alike.
import React, { useCallback, useEffect, useState } from 'react';
import { planAPI, PlanScheduleInput, ProjectPlan, todosAPI, workItemsAPI } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { TaskPanel } from '../plan/TaskPanel';
import { ErrorBanner, mutedStyle } from '../ui';

interface CardPlanProps {
  projectId: string;
  workItemId: string;
  /** Changes when the card itself changed, so the plan is read again. */
  version: number;
  /** After a write here: the board reads its cards again. */
  onChanged: () => void;
  /** Opens another task of the plan, a sub-task or a related one, as the card. */
  onSelectTask: (workItemId: string) => void;
  onClose: () => void;
}

export const CardPlan: React.FC<CardPlanProps> = ({ projectId, workItemId, version, onChanged, onSelectTask, onClose }) => {
  const [plan, setPlan] = useState<ProjectPlan | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setPlan((await planAPI.get(projectId)).data);
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to load the plan'));
    }
  }, [projectId]);

  useEffect(() => {
    void load();
  }, [load, version]);

  /** Runs a write, then reads the plan and the board again; the banner says what failed. */
  const run = async (fn: () => Promise<unknown>, fallback: string) => {
    setBusy(true);
    setError('');
    try {
      await fn();
    } catch (err) {
      setError(apiErrorMessage(err, fallback));
    } finally {
      await load();
      setBusy(false);
      onChanged();
    }
  };

  const task = plan?.tasks.find((t) => t.id === workItemId);
  return (
    <>
      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ margin: 12 }} />
      {!plan && !error && <div style={{ ...mutedStyle, padding: 12 }}>Loading the plan…</div>}
      {plan && task && (
        <TaskPanel
          plan={plan}
          task={task}
          projectId={projectId}
          editable
          busy={busy}
          planLink
          onSchedule={(t, change: PlanScheduleInput) => run(() => planAPI.schedule(t.id, change), 'Failed to schedule the task')}
          onReload={() => void load()}
          onRelate={(t, otherId, view, lagDays, lagUnit) => run(() => planAPI.relate(t.id, otherId, view.type, view.direction, lagDays, lagUnit), 'Failed to relate the tasks')}
          onUnrelate={(t, otherId) => run(() => planAPI.unrelate(t.id, otherId), 'Failed to remove the relationship')}
          onAddSubtask={(t, title) =>
            run(async () => {
              const created = await workItemsAPI.create(projectId, { title, column: 'todo' });
              await planAPI.schedule(created.data.id, { parent_id: t.id });
            }, 'Failed to add the task')
          }
          onAddTodo={(t, text, due) => run(() => planAPI.addTodo(t.id, text, due), 'Failed to add the to-do')}
          onTickTodo={(id, done) => run(() => todosAPI.setDone(id, done), 'Failed to save the to-do')}
          onSelectTask={(t) => onSelectTask(t.id)}
          onClose={onClose}
        />
      )}
    </>
  );
};

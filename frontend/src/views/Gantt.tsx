// The Plan page: a project's plan as a Gantt. Its tasks are the board's
// cards, drawn on a timeline with their sub-tasks, dependencies and progress
// (from to-dos ticked off and sub-tasks done); its milestones are the
// project's releases and the ones the team adds, each with criteria measured
// from the project's artifacts, and the band between two milestones fills
// with the later one's progress. See internal/domain/plan.
//
// In the redesign kit (components/ui): a line of figures and the controls,
// then the chart as one panel, with the selected task or milestone in a
// panel beside it. On a phone the same plan is a schedule list
// (components/plan/PlanPhone.tsx) and the selection opens in a sheet: a
// timeline needs width a phone does not have. On a touch tablet the chart
// shows but bars do not drag; dates are set in the task's panel.
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  planAPI,
  PlanExportFormat,
  PlanMilestone,
  PlanMilestoneInput,
  PlanScheduleInput,
  PlanTask,
  ProjectPlan,
  todosAPI,
  workItemsAPI,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { GanttChart } from '../components/plan/GanttChart';
import { ZOOMS, Zoom, fitZoom } from '../components/plan/ganttScale';
import { MilestonePanel } from '../components/plan/MilestonePanel';
import { AddMilestoneForm, AddTaskForm, ExportControl, NewTask } from '../components/plan/PlanForms';
import { PlanPhone } from '../components/plan/PlanPhone';
import { TaskPanel } from '../components/plan/TaskPanel';
import { ErrorBanner, FeatureGate, PanelHeader, SegmentedControl, Sheet, buttonStyle, mutedStyle, pageType, panelStyle, useConfirm } from '../components/ui';
import { GANTT_PLAN_FEATURE } from '../features';
import { useViewport } from '../hooks/useViewport';

type Selection = { kind: 'task'; id: string } | { kind: 'milestone'; key: string } | null;

const Figure: React.FC<{ value: React.ReactNode; label: string; color?: string }> = ({ value, label, color }) => (
  <span style={{ display: 'inline-flex', alignItems: 'baseline', gap: 5 }}>
    <b style={{ fontSize: 16, color: color || 'var(--text)' }}>{value}</b>
    <span style={mutedStyle}>{label}</span>
  </span>
);

const PlanPage: React.FC = () => {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const { isPhone, isCompact, coarsePointer, width } = useViewport();
  const confirm = useConfirm();

  const [plan, setPlan] = useState<ProjectPlan | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [zoom, setZoom] = useState<Zoom | null>(null);
  const [adding, setAdding] = useState<'task' | 'milestone' | null>(null);
  const [selection, setSelection] = useState<Selection>(null);

  const load = useCallback(async () => {
    if (!projectId) return;
    try {
      const res = await planAPI.get(projectId);
      setPlan(res.data);
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to load the plan'));
    }
  }, [projectId]);

  useEffect(() => {
    setPlan(null);
    setSelection(null);
    setZoom(null);
    void load();
  }, [load]);

  // The first zoom is the closest one that shows the whole plan.
  useEffect(() => {
    if (plan && zoom === null) setZoom(fitZoom(plan.start, plan.end, Math.max(width - (isCompact ? 260 : 440), 320)));
  }, [plan, zoom, width, isCompact]);

  /** Runs a write, then reads the plan again; the error banner says what failed. */
  const run = async (fn: () => Promise<unknown>, fallback: string): Promise<boolean> => {
    setBusy(true);
    setError('');
    try {
      await fn();
      await load();
      return true;
    } catch (err) {
      setError(apiErrorMessage(err, fallback));
      void load();
      return false;
    } finally {
      setBusy(false);
    }
  };

  const schedule = (t: PlanTask, change: PlanScheduleInput) => run(() => planAPI.schedule(t.id, change), 'Failed to schedule the task');

  const addTask = async (t: NewTask, parentId?: string) => {
    const ok = await run(async () => {
      const created = await workItemsAPI.create(projectId, { title: t.title, column: 'todo' });
      const change: PlanScheduleInput = {
        ...(t.start ? { start_date: t.start } : {}),
        ...(t.end ? { due_date: t.end } : {}),
        ...(parentId ? { parent_id: parentId } : {}),
      };
      if (Object.keys(change).length > 0) await planAPI.schedule(created.data.id, change);
      if (!parentId) setSelection({ kind: 'task', id: created.data.id });
    }, 'Failed to add the task');
    if (ok && !parentId) setAdding(null);
  };

  const addMilestone = async (m: PlanMilestoneInput) => {
    const ok = await run(async () => {
      const created = await planAPI.createMilestone(projectId, m);
      setSelection({ kind: 'milestone', key: created.data.id });
    }, 'Failed to add the milestone');
    if (ok) setAdding(null);
  };

  const saveMilestone = (m: PlanMilestone, change: PlanMilestoneInput) =>
    run(
      () =>
        m.kind === 'release'
          ? planAPI.setReleaseCriteria(projectId, m.release_id!, change.criteria || [])
          : planAPI.updateMilestone(projectId, m.id, change),
      'Failed to save the milestone',
    );

  const deleteMilestone = async (m: PlanMilestone) => {
    const release = m.kind === 'release';
    const ok = await confirm({
      title: release ? 'Reset criteria' : 'Delete milestone',
      message: release
        ? `Put ${m.name}'s criteria back to the defaults: every requirement in the release approved and verified?`
        : `Delete the milestone ${m.name}? Its criteria go with it; no task or artifact changes.`,
      confirmLabel: release ? 'Reset to defaults' : 'Delete milestone',
      danger: !release,
    });
    if (!ok) return;
    if (await run(() => planAPI.removeMilestone(projectId, m.id), 'Failed to delete the milestone')) {
      if (!release) setSelection(null);
    }
  };

  const exportPlan = async (format: PlanExportFormat) => {
    setExporting(true);
    setError('');
    try {
      await planAPI.export(projectId, format);
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to export the plan'));
    } finally {
      setExporting(false);
    }
  };

  const selectedTask = useMemo(
    () => (selection?.kind === 'task' && plan ? plan.tasks.find((t) => t.id === selection.id) || null : null),
    [selection, plan],
  );
  const selectedMilestone = useMemo(
    () => (selection?.kind === 'milestone' && plan ? plan.milestones.find((m) => m.key === selection.key) || null : null),
    [selection, plan],
  );
  const openTask = (t: PlanTask) => setSelection({ kind: 'task', id: t.id });
  const openMilestone = (m: PlanMilestone) => setSelection({ kind: 'milestone', key: m.key });
  const close = () => setSelection(null);

  const errorBanner = <ErrorBanner message={error} onDismiss={() => setError('')} />;
  if (!plan) {
    return (
      <div style={{ ...pageType(isPhone), padding: 16 }}>
        {errorBanner}
        {!error && <div style={mutedStyle}>Loading…</div>}
      </div>
    );
  }

  const controls = (
    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
      {adding !== 'task' && (
        <button type="button" onClick={() => setAdding('task')} data-help-key="gantt.add-task" style={buttonStyle('primary')}>
          Add task
        </button>
      )}
      {adding !== 'milestone' && (
        <button type="button" onClick={() => setAdding('milestone')} data-help-key="gantt.add-milestone" style={buttonStyle('secondary')}>
          Add milestone
        </button>
      )}
      <ExportControl busy={exporting} onExport={exportPlan} />
    </div>
  );
  const forms = (
    <>
      {adding === 'task' && <AddTaskForm busy={busy} onSubmit={(t) => addTask(t)} onCancel={() => setAdding(null)} />}
      {adding === 'milestone' && <AddMilestoneForm plan={plan} busy={busy} onSubmit={addMilestone} onCancel={() => setAdding(null)} />}
    </>
  );
  const panel = selectedTask ? (
    <TaskPanel
      plan={plan}
      task={selectedTask}
      projectId={projectId}
      editable
      busy={busy}
      onSchedule={schedule}
      onAddTodo={(t, text) => run(() => planAPI.addTodo(t.id, text), 'Failed to add the to-do')}
      onTickTodo={(id, done) => run(() => todosAPI.setDone(id, done), 'Failed to save the to-do')}
      onAddSubtask={(t, title) => addTask({ title, start: '', end: '' }, t.id)}
      onSelectTask={openTask}
      onClose={close}
    />
  ) : selectedMilestone ? (
    <MilestonePanel
      plan={plan}
      milestone={selectedMilestone}
      projectId={projectId}
      editable
      busy={busy}
      onSave={saveMilestone}
      onDelete={deleteMilestone}
      onClose={close}
    />
  ) : null;

  if (isPhone) {
    return (
      <>
        <PlanPhone plan={plan} head={controls} forms={forms} error={errorBanner} onOpenTask={openTask} onOpenMilestone={openMilestone} />
        {panel && (
          <Sheet label={selectedTask ? `Task ${selectedTask.title}` : `Milestone ${selectedMilestone?.name}`} onClose={close}>
            {panel}
          </Sheet>
        )}
      </>
    );
  }

  const s = plan.summary;
  const undated = plan.milestones.filter((m) => !m.date);
  return (
    <div style={{ ...pageType(false), padding: 16, display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px 20px', flexWrap: 'wrap' }}>
        <div data-help-key="gantt.summary" style={{ display: 'flex', gap: '4px 20px', flexWrap: 'wrap', flex: '1 1 320px' }}>
          <Figure value={`${s.progress}%`} label="complete" />
          <Figure value={s.tasks} label={s.tasks === 1 ? 'task' : 'tasks'} />
          <Figure value={s.done} label="done" color="var(--success-text)" />
          <Figure value={s.late} label="late" color={s.late ? 'var(--danger-strong)' : undefined} />
          {s.unscheduled > 0 && <Figure value={s.unscheduled} label="not scheduled" />}
          <Figure value={`${plan.milestones.filter((m) => m.met).length}/${plan.milestones.length}`} label="milestones met" />
        </div>
        {controls}
      </div>
      {errorBanner}
      {forms}
      <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
        <section aria-label="Plan" style={{ ...panelStyle, flex: 1, overflow: 'hidden' }}>
          <PanelHeader
            title="Plan"
            action={
              <SegmentedControl
                aria-label="Zoom"
                options={ZOOMS}
                value={zoom || 'weeks'}
                onChange={setZoom}
                style={{ fontSize: 12 }}
              />
            }
          />
          <GanttChart
            plan={plan}
            zoom={zoom || 'weeks'}
            outlineWidth={isCompact ? 220 : 320}
            selectedTaskId={selectedTask?.id || ''}
            selectedMilestoneKey={selectedMilestone?.key || ''}
            onSelectTask={openTask}
            onSelectMilestone={openMilestone}
            onReschedule={coarsePointer ? undefined : (t, start, end) => schedule(t, { start_date: start, due_date: end })}
          />
          {undated.length > 0 && (
            <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap', padding: '8px 12px', borderTop: '1px solid var(--border-soft)' }}>
              <span style={mutedStyle}>No date yet:</span>
              {undated.map((m) => (
                <button key={m.key} type="button" onClick={() => openMilestone(m)} style={buttonStyle('secondary', { compact: true })}>
                  ◆ {m.name} · {m.progress}%
                </button>
              ))}
            </div>
          )}
          <p style={{ ...mutedStyle, margin: 0, padding: '8px 12px', borderTop: '1px solid var(--border-soft)' }}>
            A task's bar fills as its to-dos are ticked off and its sub-tasks finish. A milestone's band fills as its
            criteria are met. {coarsePointer ? 'Select a task to set its dates.' : 'Drag a bar to move it, or an end to resize it.'}
          </p>
        </section>
        {panel && !isCompact && <div style={{ width: 340, flexShrink: 0 }}>{panel}</div>}
      </div>
      {panel && isCompact && (
        <Sheet label={selectedTask ? `Task ${selectedTask.title}` : `Milestone ${selectedMilestone?.name}`} onClose={close}>
          {panel}
        </Sheet>
      )}
    </div>
  );
};

export const Gantt: React.FC = () => (
  <FeatureGate feature={GANTT_PLAN_FEATURE} name="Gantt">
    <PlanPage />
  </FeatureGate>
);

// The Plan page: a project's plan as a Gantt. Its tasks are the board's
// cards, drawn on a timeline with their sub-tasks, their relationships
// (finish to start, start to start, finish to finish) and progress (from the
// to-dos ticked off on the card and sub-tasks done); its milestones are the
// project's releases and the ones the team adds, each with criteria measured
// from the project's artifacts, and the band between two milestones fills
// with the later one's progress. See internal/domain/plan.
//
// In the redesign kit (components/ui): a line of figures and the controls,
// then the chart as one panel, with the selected task or milestone in a
// panel beside it. On a phone the same plan is a schedule list
// (components/plan/PlanPhone.tsx) and the selection opens in a sheet: a
// timeline needs width a phone does not have. On a touch tablet the chart
// shows but bars do not drag; dates and relationships are set in the task's
// panel. The plan's working days (Monday to Friday until set) decide which
// days a duration counts and which the chart shades.
import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  Artifact,
  artifactAPI,
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
import { WorkItemDrawer } from '../components/kanban/WorkItemDrawer';
import { Zoom, fitZoom } from '../components/plan/ganttScale';
import { MilestonePanel } from '../components/plan/MilestonePanel';
import { AddMilestoneForm, AddTaskForm, ExportControl, NewTask } from '../components/plan/PlanForms';
import { PlanPhone } from '../components/plan/PlanPhone';
import { PlanChartPanel } from '../components/plan/PlanChartPanel';
import { TaskPanel } from '../components/plan/TaskPanel';
import type { RelationshipView } from '../components/plan/planModel';
import { ErrorBanner, FeatureGate, Sheet, buttonStyle, mutedStyle, pageType, useConfirm } from '../components/ui';
import { GANTT_PLAN_FEATURE } from '../features';
import { useViewport } from '../hooks/useViewport';

type Selection = { kind: 'task'; id: string } | { kind: 'milestone'; key: string } | null;

const Figure: React.FC<{ value: React.ReactNode; label: string; color?: string }> = ({ value, label, color }) => (
  <span style={{ display: 'inline-flex', alignItems: 'baseline', gap: 5 }}>
    <b style={{ fontSize: 16, color: color || 'var(--text)' }}>{value}</b>
    <span style={mutedStyle}>{label}</span>
  </span>
);

/**
 * The height that takes an element from where it sits to the bottom of the
 * window, so the chart and the panel beside it fill the page however few
 * tasks the plan has. Measured again on every render and resize, since a
 * form or a banner above moves the element down.
 */
const useFillHeight = (min: number): [React.RefObject<HTMLDivElement>, number] => {
  const ref = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(min);
  const measure = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const top = el.getBoundingClientRect().top + (document.scrollingElement?.scrollTop ?? 0);
    const next = Math.max(Math.floor(window.innerHeight - top - 16), min);
    setHeight((h) => (h === next ? h : next));
  }, [min]);
  useLayoutEffect(measure);
  useEffect(() => {
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, [measure]);
  return [ref, height];
};

const PlanPage: React.FC = () => {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const { isPhone, isCompact, coarsePointer, width } = useViewport();
  const confirm = useConfirm();
  const [fill, fillHeight] = useFillHeight(360);

  const [plan, setPlan] = useState<ProjectPlan | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [zoom, setZoom] = useState<Zoom | null>(null);
  const [adding, setAdding] = useState<'task' | 'milestone' | null>(null);
  const [selection, setSelection] = useState<Selection>(null);
  // The task opened in full, in the board's drawer, and the artifacts it names.
  const [note, setNote] = useState('');
  const [fullTaskId, setFullTaskId] = useState('');
  const [artifactMap, setArtifactMap] = useState<Record<string, Artifact> | null>(null);
  useEffect(() => {
    if (!fullTaskId || artifactMap || !projectId) return;
    artifactAPI
      .list(projectId)
      .then((res) => setArtifactMap(Object.fromEntries((res.data || []).map((a) => [a.id, a]))))
      .catch(() => setArtifactMap({}));
  }, [fullTaskId, artifactMap, projectId]);

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
    if (plan && zoom === null) setZoom(fitZoom(plan.start, plan.end, Math.max(width - (isCompact ? 260 : 460), 320)));
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

  const relate = (taskId: string, otherId: string, view: RelationshipView, lagDays = 0) =>
    run(() => planAPI.relate(taskId, otherId, view.type, view.direction, lagDays), 'Failed to relate the tasks');

  /** Moves the driven tasks to what their relationships ask, once the reader agrees, and says how many moved. */
  const recalculate = async () => {
    const ok = await confirm({
      title: 'Recalculate the plan',
      message:
        'Move every task that another task drives to the dates its relationships and their offsets ask for? Each keeps its duration. Tasks nothing drives, and tasks that are done, stay where they are.',
      confirmLabel: 'Recalculate',
    });
    if (!ok) return;
    setNote('');
    await run(async () => {
      const res = await planAPI.recalculate(projectId);
      const n = res.data.moved;
      setNote(n === 0 ? 'Nothing moved: every task already sits where its relationships ask.' : `${n} ${n === 1 ? 'task' : 'tasks'} moved.`);
    }, 'Failed to recalculate the plan');
  };

  const addTask = async (t: NewTask, parentId?: string, inline = false) => {
    const ok = await run(async () => {
      const created = await workItemsAPI.create(projectId, { title: t.title, column: 'todo' });
      const change: PlanScheduleInput = {
        ...(t.start ? { start_date: t.start } : {}),
        ...(t.end ? { due_date: t.end } : {}),
        ...(parentId ? { parent_id: parentId } : {}),
      };
      if (Object.keys(change).length > 0) await planAPI.schedule(created.data.id, change);
      if (!parentId && !inline) setSelection({ kind: 'task', id: created.data.id });
    }, 'Failed to add the task');
    if (ok && !parentId && !inline) setAdding(null);
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
  const openFull = (t: PlanTask) => setFullTaskId(t.id);
  const fullTask = fullTaskId ? (
    <WorkItemDrawer
      workItemId={fullTaskId}
      projectId={projectId}
      artifactMap={artifactMap || {}}
      onClose={() => setFullTaskId('')}
      onChanged={() => void load()}
      onDeleted={() => {
        setFullTaskId('');
        setSelection(null);
        void load();
      }}
    />
  ) : null;

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
      onRelate={(t, otherId, view, lagDays) => relate(t.id, otherId, view, lagDays)}
      onUnrelate={(t, otherId) => run(() => planAPI.unrelate(t.id, otherId), 'Failed to remove the relationship')}
      onAddSubtask={(t, title) => addTask({ title, start: '', end: '' }, t.id)}
      onAddTodo={(t, text, due) => run(() => planAPI.addTodo(t.id, text, due), 'Failed to add the to-do')}
      onTickTodo={(id, done) => run(() => todosAPI.setDone(id, done), 'Failed to save the to-do')}
      onSelectTask={openTask}
      onOpenTask={openFull}
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
      onAssign={(taskId, key) => run(() => planAPI.schedule(taskId, { milestone: key }), 'Failed to assign the task')}
      onSelectTask={openTask}
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
        {fullTask}
      </>
    );
  }

  const s = plan.summary;
  return (
    <div style={{ ...pageType(false), padding: 16, display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px 20px', flexWrap: 'wrap' }}>
        <div data-help-key="gantt.summary" style={{ display: 'flex', gap: '4px 20px', flexWrap: 'wrap', flex: '1 1 320px' }}>
          <Figure value={`${s.progress}%`} label="complete" />
          <Figure value={s.tasks} label={s.tasks === 1 ? 'task' : 'tasks'} />
          <Figure value={s.done} label="done" color="var(--success-text)" />
          <Figure value={s.late} label="late" color={s.late ? 'var(--danger-strong)' : undefined} />
          <Figure value={s.critical} label="on the critical path" color={s.critical ? 'var(--warning-text)' : undefined} />
          {s.unscheduled > 0 && <Figure value={s.unscheduled} label="not scheduled" />}
          {s.effort_days > 0 && <Figure value={s.effort_days} label={s.effort_days === 1 ? 'day of effort' : 'days of effort'} />}
          <Figure value={`${plan.milestones.filter((m) => m.met).length}/${plan.milestones.length}`} label="milestones met" />
        </div>
        {controls}
      </div>
      {errorBanner}
      {note && (
        <div role="status" style={{ ...mutedStyle, display: 'flex', alignItems: 'center', gap: 8 }}>
          {note}
          <button type="button" onClick={() => setNote('')} aria-label="Dismiss" style={buttonStyle('ghost', { compact: true })}>
            ✕
          </button>
        </div>
      )}
      {forms}
      <div ref={fill} style={{ display: 'flex', gap: 12, alignItems: 'stretch', height: fillHeight }}>
        <PlanChartPanel
          plan={plan}
          zoom={zoom || 'weeks'}
          onZoom={setZoom}
          onWorkingDays={(days) => run(() => planAPI.setWorkingDays(projectId, days), 'Failed to set the working days')}
          outlineWidth={isCompact ? 220 : 320}
          selectedTaskId={selectedTask?.id || ''}
          selectedMilestoneKey={selectedMilestone?.key || ''}
          onSelectTask={openTask}
          onOpenTask={openFull}
          onSelectMilestone={openMilestone}
          onReschedule={coarsePointer ? undefined : (t, start, end) => schedule(t, { start_date: start, due_date: end })}
          onLink={coarsePointer ? undefined : relate}
          onRecalculate={recalculate}
          onAddTask={(title) => addTask({ title, start: '', end: '' }, undefined, true)}
          busy={busy}
          touch={coarsePointer}
        />
        {panel && !isCompact && (
          <div data-plan-side style={{ width: 340, flexShrink: 0, display: 'flex', flexDirection: 'column', minHeight: 0, overflowY: 'auto' }}>{panel}</div>
        )}
      </div>
      {panel && isCompact && (
        <Sheet label={selectedTask ? `Task ${selectedTask.title}` : `Milestone ${selectedMilestone?.name}`} onClose={close}>
          {panel}
        </Sheet>
      )}
      {fullTask}
    </div>
  );
};

export const Gantt: React.FC = () => (
  <FeatureGate feature={GANTT_PLAN_FEATURE} name="Gantt">
    <PlanPage />
  </FeatureGate>
);

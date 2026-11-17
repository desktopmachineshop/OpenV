// Undo and redo for edits to a task's schedule on the Plan page. Each edit
// is remembered with what puts the task back as it was; undoing sends that,
// redoing sends the edit again. Relationships made by dragging, milestones
// and the plan's settings are not remembered.
import { useCallback, useEffect, useRef, useState } from 'react';
import type { PlanScheduleInput, PlanTask } from '../../api/client';

interface Edit {
  id: string;
  undo: PlanScheduleInput;
  redo: PlanScheduleInput;
}

const HISTORY = 50;

/** The change that puts back what an edit to a task changes. */
export const inverseOf = (tasks: PlanTask[], t: PlanTask, change: PlanScheduleInput): PlanScheduleInput => {
  const back: PlanScheduleInput = {};
  const has = (k: keyof PlanScheduleInput) => k in change;
  if (has('start_date') || has('due_date') || has('duration_days')) {
    back.start_date = t.start || '';
    back.due_date = t.end || '';
    if (!t.start || !t.end) back.duration_days = t.duration_days || 0;
  }
  if (has('effort_days')) back.effort_days = t.effort_days || 0;
  if (has('elapsed')) back.elapsed = t.elapsed;
  if (has('deadline')) back.deadline = t.deadline || '';
  if (has('mode')) back.mode = t.mode_own;
  if (has('constraint_type') || has('constraint_date')) {
    back.constraint_type = t.constraint_type;
    if (t.constraint_date) back.constraint_date = t.constraint_date;
  }
  if (has('assignments')) back.assignments = t.assignments;
  if (has('fixed_cost')) back.fixed_cost = t.fixed_cost;
  if (has('effort_driven')) back.effort_driven = t.effort_driven;
  if (has('actual_start')) back.actual_start = t.actual_start || '';
  if (has('actual_finish')) back.actual_finish = t.actual_finish || '';
  if (has('percent_complete')) back.percent_complete = t.percent_complete ?? -1;
  if (has('actual_cost')) back.actual_cost = t.actual_cost ?? -1;
  if (has('milestone')) back.milestone = t.milestone_inherited ? '' : t.milestone || '';
  if (has('parent_id') || has('after_id')) {
    const siblings = tasks.filter((x) => x.parent_id === t.parent_id);
    const at = siblings.findIndex((x) => x.id === t.id);
    back.parent_id = t.parent_id || '';
    back.after_id = at > 0 ? siblings[at - 1].id : '';
  }
  if (has('relationships') || has('depends_on')) back.relationships = t.relationships.map((r) => ({ task_id: r.task_id, type: r.type, lag_days: r.lag_days, lag_unit: r.lag_unit }));
  return back;
};

export interface PlanHistory {
  /** Makes an edit and remembers how to undo it. */
  schedule: (t: PlanTask, change: PlanScheduleInput) => Promise<boolean>;
  undo: () => void;
  redo: () => void;
  canUndo: boolean;
  canRedo: boolean;
}

/**
 * The page's schedule edits with their history. send makes one edit and
 * answers whether it was saved. Ctrl+Z undoes and Ctrl+Y, or Ctrl+Shift+Z,
 * redoes, unless the keys are typed in a field.
 */
export const usePlanHistory = (tasks: PlanTask[] | undefined, send: (id: string, change: PlanScheduleInput) => Promise<boolean>): PlanHistory => {
  const [past, setPast] = useState<Edit[]>([]);
  const [future, setFuture] = useState<Edit[]>([]);
  const live = useRef({ past, future, send });
  live.current = { past, future, send };

  const schedule = useCallback(
    async (t: PlanTask, change: PlanScheduleInput) => {
      const edit: Edit = { id: t.id, undo: inverseOf(tasks || [], t, change), redo: change };
      const ok = await live.current.send(t.id, change);
      if (ok) {
        setPast((p) => [...p.slice(-(HISTORY - 1)), edit]);
        setFuture([]);
      }
      return ok;
    },
    [tasks],
  );
  const undo = useCallback(() => {
    const edit = live.current.past[live.current.past.length - 1];
    if (!edit) return;
    setPast((p) => p.slice(0, -1));
    setFuture((f) => [...f, edit]);
    void live.current.send(edit.id, edit.undo);
  }, []);
  const redo = useCallback(() => {
    const edit = live.current.future[live.current.future.length - 1];
    if (!edit) return;
    setFuture((f) => f.slice(0, -1));
    setPast((p) => [...p, edit]);
    void live.current.send(edit.id, edit.redo);
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName))) return;
      const key = e.key.toLowerCase();
      if (key === 'z' && !e.shiftKey) undo();
      else if (key === 'y' || (key === 'z' && e.shiftKey)) redo();
      else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [undo, redo]);

  return { schedule, undo, redo, canUndo: past.length > 0, canRedo: future.length > 0 };
};

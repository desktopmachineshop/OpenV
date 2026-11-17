// Picking more than one task on the plan's charts, as a file manager picks
// files: a plain click picks one, Ctrl or Cmd adds or drops one, Shift
// takes the run from the last one picked.
import { useCallback, useState } from 'react';

/** The modifier keys of the click, which a mouse event carries. */
export interface PickKeys {
  ctrlKey?: boolean;
  metaKey?: boolean;
  shiftKey?: boolean;
}

export interface Picked {
  ids: string[];
  /** Where a Shift-click's run starts: the last task picked without Shift. */
  anchor: string;
}

/** What a click on a task makes of what was picked; order is the tasks' outline order. */
export const pickTask = (order: string[], was: Picked, id: string, keys: PickKeys = {}): Picked => {
  if (keys.shiftKey && was.anchor && order.includes(was.anchor)) {
    const [from, to] = [order.indexOf(was.anchor), order.indexOf(id)].sort((a, b) => a - b);
    const run = order.slice(from, to + 1);
    // Ctrl with Shift adds the run to what is picked.
    return { ids: keys.ctrlKey || keys.metaKey ? Array.from(new Set([...was.ids, ...run])) : run, anchor: was.anchor };
  }
  if (keys.ctrlKey || keys.metaKey) {
    return { ids: was.ids.includes(id) ? was.ids.filter((x) => x !== id) : [...was.ids, id], anchor: id };
  }
  return { ids: [id], anchor: id };
};

/** The tasks picked on a plan page; `current` is the one task open beside the chart, which a pick starts from. */
export const usePlanPicks = (order: string[], current: string) => {
  const [picked, setPicked] = useState<Picked>({ ids: [], anchor: '' });
  // One task open beside the chart is the whole pick, however it was opened.
  const was: Picked = current ? { ids: [current], anchor: current } : picked;
  /** Takes the click, and names the task left picked alone, if one is: it opens beside the chart. */
  const pick = (id: string, keys?: PickKeys): string => {
    const next = pickTask(order, was, id, keys);
    setPicked(next);
    return next.ids.length === 1 ? next.ids[0] : '';
  };
  const clear = useCallback(() => setPicked({ ids: [], anchor: '' }), []);
  const ids = was.ids.filter((id) => order.includes(id));
  return { ids, pick, clear };
};

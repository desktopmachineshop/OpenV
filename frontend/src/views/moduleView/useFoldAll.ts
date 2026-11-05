import { useState } from 'react';
import type { RequirementsView } from './requirementsViews';

export interface FoldTrigger {
  /** True to fold every level, false to open every level. */
  folded: boolean;
  /** Changes on every press, 0 before the first: the view applies a new token once. */
  token: number;
}

/**
 * The fold-all / unfold-all button's state, per view. The Table opens
 * unfolded, so its button starts as Fold all; the Tree diagram (and a
 * phone's outline) opens folded to two levels, so its button starts as
 * Unfold all. A press turns the view in front the other way; switching view
 * starts the other one afresh.
 */
export function useFoldAll(view: RequirementsView) {
  const [state, setState] = useState<{ view: RequirementsView; folded: boolean; token: number }>({ view, folded: false, token: 0 });
  const mine = state.view === view;
  const folded = mine ? state.folded : view === 'tree';
  const trigger: FoldTrigger = { folded, token: mine ? state.token : 0 };
  const toggleFold = () => setState((prev) => ({ view, folded: !folded, token: prev.token + 1 }));
  return { folded, trigger, toggleFold };
}

import { useEffect, useState } from 'react';
import {
  PanelMode,
  loadPanelMode,
  nextPanelMode,
  panelIsOpen,
  panelTakesSpace,
  revealAfterModeChange,
  savePanelMode,
} from '../../components/panelMode';
import type { Setter } from './shared';

/**
 * How much room a side panel takes (components/panelMode.ts): pinned,
 * auto-hidden or hidden, remembered per panel, with the pointer's hover and
 * an explicit reveal from the edge strip. `fallback` gives the mode for a
 * reader who never chose one; it is called once, when the mode is first read.
 */
export function usePanelMode(panel: string, fallback: () => PanelMode) {
  const [mode, setMode] = useState<PanelMode>(() => loadPanelMode(panel, fallback()));
  const [hovered, setHovered] = useState(false);
  // An explicit "show it to me" from the edge strip. Hover alone never opens a
  // hidden panel, so this is the only way back from that mode, and it
  // stays open until dismissed rather than vanishing when the pointer moves.
  const [revealed, setRevealed] = useState(false);

  const open = panelIsOpen(mode, hovered, revealed);
  const pinned = panelTakesSpace(mode);

  const cycleMode = () => {
    const next = nextPanelMode(mode);
    setMode(next);
    savePanelMode(panel, next);
    setHovered(false);
    // Keep the panel on screen while its mode is being chosen, so the button
    // doing the choosing does not disappear mid-cycle (issue #362).
    setRevealed(revealAfterModeChange(next));
  };

  return { mode, hovered, setHovered, revealed, setRevealed, open, pinned, cycleMode };
}

/**
 * Escape dismisses a revealed panel. A hook of its own, beside usePanelMode,
 * so the shell can keep the listener's effect where it stands among its
 * other effects.
 */
export function usePanelEscape(revealed: boolean, setRevealed: Setter<boolean>) {
  useEffect(() => {
    if (!revealed) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setRevealed(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // setRevealed is usePanelMode's state setter, which React keeps stable,
    // so the array stays [revealed], as it was in ModuleView.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [revealed]);
}

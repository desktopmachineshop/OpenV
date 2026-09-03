// How much room a side panel is allowed to take.
//
// The requirements page is a document, and a document wants width. The project
// nav and the notes panel are both useful occasionally and expensive
// permanently, so each remembers one of three states rather than only open or
// shut:
//
//   pinned   — always open, the old behaviour
//   autohide — a thin edge strip that opens while the pointer is over it and
//              closes when it leaves, so the space is borrowed, not spent
//   hidden   — gone until the reader asks for it back
//
// The choice is per person and per panel and survives a reload, because it is
// a working preference rather than something to re-pick on every visit.

export type PanelMode = 'pinned' | 'autohide' | 'hidden';

export const PANEL_MODES: PanelMode[] = ['pinned', 'autohide', 'hidden'];

/** What each mode is called where a reader chooses it. */
export const panelModeLabel = (mode: PanelMode): string =>
  mode === 'pinned' ? 'Pinned' : mode === 'autohide' ? 'Auto-hide' : 'Hidden';

/** The icon a toggle shows for the mode it would move to. */
export const panelModeIcon = (mode: PanelMode): string =>
  mode === 'pinned' ? '📌' : mode === 'autohide' ? '↔' : '⨯';

/** The next mode when a reader clicks through the toggle. */
export const nextPanelMode = (mode: PanelMode): PanelMode =>
  mode === 'pinned' ? 'autohide' : mode === 'autohide' ? 'hidden' : 'pinned';

/**
 * Whether the panel's content occupies the layout right now.
 *
 * Pinned always does. Auto-hide does only while hovered, and then it overlays
 * rather than pushing the document around: a panel that reflows the text every
 * time the pointer drifts past would be worse than one that stays shut.
 */
export const panelIsOpen = (mode: PanelMode, hovered: boolean): boolean =>
  mode === 'pinned' || (mode === 'autohide' && hovered);

/** Whether the panel takes width from the document rather than floating over it. */
export const panelTakesSpace = (mode: PanelMode): boolean => mode === 'pinned';

const storageKey = (panel: string) => `openv-panel-mode-${panel}`;

/**
 * The stored mode for a panel, defaulting to pinned so nothing moves for
 * someone who has never chosen. Unreadable or unknown values fall back the
 * same way rather than throwing: a broken preference should not break the page.
 */
export const loadPanelMode = (panel: string, fallback: PanelMode = 'pinned'): PanelMode => {
  try {
    const raw = localStorage.getItem(storageKey(panel));
    return PANEL_MODES.includes(raw as PanelMode) ? (raw as PanelMode) : fallback;
  } catch {
    return fallback;
  }
};

/** Remember a panel's mode. Storage failures are ignored — it is a preference. */
export const savePanelMode = (panel: string, mode: PanelMode): void => {
  try {
    localStorage.setItem(storageKey(panel), mode);
  } catch {
    /* a preference that cannot be stored is not worth an error */
  }
};

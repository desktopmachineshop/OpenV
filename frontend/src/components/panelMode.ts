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
 *
 * `revealed` is the reader asking for the panel back by clicking its edge
 * strip. It is what makes "hidden" a state you can leave: hovering a hidden
 * panel deliberately does nothing — that is the difference between hidden and
 * auto-hide — so without an explicit reveal there is no way back short of
 * cycling the mode, and the strip becomes a button that does nothing. A
 * revealed panel stays open until it is dismissed rather than closing when the
 * pointer wanders off, because someone who asked for it is about to use it.
 */
export const panelIsOpen = (mode: PanelMode, hovered: boolean, revealed = false): boolean =>
  mode === 'pinned' || revealed || (mode === 'autohide' && hovered);

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

/**
 * How wide the edge strip that brings a panel back should be.
 *
 * A strip is the ONLY way back from `hidden`, so it has to read as a control
 * rather than as a border. Ten pixels of chrome with a 12px chevron in it was
 * neither: people could not find it, and those who did had to aim at it
 * (issue #363). It is now a comfortable click target on a mouse and a
 * comfortable tap target on a touch screen, where there is no hover to hint
 * that anything is there at all.
 */
export const panelStripWidth = (coarsePointer: boolean): number => (coarsePointer ? 32 : 24);

/**
 * What the strip says it will do, for its tooltip and its accessible name.
 * The panel's name is the subject because the strip sits away from the panel
 * it controls — once the panel is hidden there is nothing beside it to say
 * what "show" means.
 */
export const panelStripLabel = (panelName: string, open: boolean): string =>
  `${open ? 'Hide' : 'Show'} ${panelName}`;

/**
 * What a mode-cycling click should leave the panel doing.
 *
 * Cycling used to close the panel on the spot, which took the button that
 * does the cycling off the screen with it: choosing "Auto-hide" or "Hidden"
 * ended the cycle whether or not that was the mode you wanted, and getting to
 * the third option meant hunting for the edge strip first (issue #362). So a
 * panel that would close stays revealed instead — the button keeps its place
 * under the pointer, and the reader dismisses the panel when they are done
 * choosing, by clicking away, pressing Escape, or clicking the strip.
 *
 * Pinned needs no reveal: it is open by definition, and carrying a stale
 * `revealed` into it would leave the next cycle unable to close anything.
 */
export const revealAfterModeChange = (next: PanelMode): boolean => next !== 'pinned';

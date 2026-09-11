// Installed-app shortcuts (REQ-109).
//
// A web app manifest shortcut has to be a STATIC url, but OpenV's review
// queue and board live under a project (/projects/:id/review, .../board), so
// neither has a static address. The shortcuts therefore point at the project
// list with a `go` parameter saying where the member was heading: the list
// jumps straight there when it knows which project they were last in, and
// otherwise asks them to pick one and then lands on that tab.
//
// The notifications shortcut needs no project — it opens the bell's panel on
// the project list via `?panel=notifications`.

/** localStorage key holding the last project the member opened. */
export const LAST_PROJECT_KEY = 'openv_last_project';

/** Query parameter naming the tab a shortcut was heading for. */
export const SHORTCUT_PARAM = 'go';

/** Query parameter (and value) that opens the notifications panel. */
export const PANEL_PARAM = 'panel';
export const PANEL_NOTIFICATIONS = 'notifications';

/** The project tabs an installed-app shortcut can target. */
export type ShortcutTarget = 'review' | 'board';

const TARGETS: Record<ShortcutTarget, string> = {
  review: 'review queue',
  board: 'board',
};

/** shortcutTarget validates a raw `?go=` value; anything else is null. */
export const shortcutTarget = (raw: string | null | undefined): ShortcutTarget | null =>
  raw === 'review' || raw === 'board' ? raw : null;

/** shortcutLabel names a target in prose ("review queue", "board"). */
export const shortcutLabel = (target: ShortcutTarget): string => TARGETS[target];

/**
 * projectPathFor builds the route a shortcut lands on inside a project.
 * A null target means the project overview, the ordinary destination.
 */
export const projectPathFor = (projectId: string, target: ShortcutTarget | null): string =>
  target ? `/projects/${projectId}/${target}` : `/projects/${projectId}`;

/**
 * rememberProject records the project a member opened so a later shortcut can
 * land in it directly. Storage can throw (private mode, blocked site data);
 * a shortcut that has to ask which project is a lesser problem than a crash.
 */
export const rememberProject = (projectId: string): void => {
  try {
    window.localStorage.setItem(LAST_PROJECT_KEY, projectId);
  } catch {
    // ignore
  }
};

/** lastProject answers the remembered project id, or ''. */
export const lastProject = (): string => {
  try {
    return window.localStorage.getItem(LAST_PROJECT_KEY) || '';
  } catch {
    return '';
  }
};

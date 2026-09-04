// Whether each group in the project menu is expanded.
//
// The menu lists every destination in the project at once — four labelled
// groups plus the unlabelled tail — which is more than fits a short window and
// more than anyone needs while working in one of them. So the groups collapse,
// and they start collapsed: the menu is then five lines instead of fifteen,
// and it opens only where the reader asks.
//
// The one exception is the group holding the page being viewed. A menu that
// hides where you are is worse than a long one, so that group opens on its own
// — until the reader collapses it, because an explicit choice is remembered and
// beats the default.
//
// Choices are per person and survive a reload, like the panel modes next door.

/** Expanded state by section label. A label absent from the map is unchosen. */
export type NavSectionState = Record<string, boolean>;

const STORAGE_KEY = 'openv-nav-sections';

/**
 * The stored expansion state, or an empty map when there is none. A broken or
 * unreadable preference falls back to the default rather than throwing: it is
 * a convenience, not something worth breaking the page over.
 */
export const loadNavSections = (): NavSectionState => {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}');
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
    const state: NavSectionState = {};
    for (const [label, open] of Object.entries(parsed)) {
      if (typeof open === 'boolean') state[label] = open;
    }
    return state;
  } catch {
    return {};
  }
};

/** Remember the expansion state. Storage failures are ignored. */
export const saveNavSections = (state: NavSectionState): void => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch {
    /* a preference that cannot be stored is not worth an error */
  }
};

/**
 * Whether a section shows its items: what the reader last chose for it, and
 * failing that, open only if the page being viewed is inside it.
 */
export const navSectionIsOpen = (
  state: NavSectionState,
  label: string,
  hasActiveItem = false
): boolean => {
  const stored = state[label];
  return typeof stored === 'boolean' ? stored : hasActiveItem;
};

/**
 * The state after clicking a section header. The caller passes the state the
 * header is currently drawn in, so clicking a section that opened itself
 * closes it in one click rather than confirming it open.
 */
export const toggleNavSection = (
  state: NavSectionState,
  label: string,
  open: boolean
): NavSectionState => ({ ...state, [label]: !open });

/**
 * The project-relative route the reader is on: '' on the overview,
 * 'requirements' inside the requirements module, and so on. Deeper paths
 * ('requirements/REQ-1') report their module, because that is what the menu
 * names.
 */
export const activeNavPath = (pathname: string, projectId: string): string => {
  const base = `/projects/${projectId}`;
  if (!projectId || !pathname.startsWith(base)) return '';
  return pathname.slice(base.length).split('/').filter(Boolean)[0] || '';
};

/** Whether one of a section's items is the page being viewed. */
export const sectionHasActive = (items: { to: string }[], activePath: string): boolean =>
  items.some((item) => item.to === activePath);

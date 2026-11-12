import type { NavItem, NavSection } from '../pages';
import { sectionHasActive } from './navSections';

// The project menu of the refreshed shell (the ui-refresh feature key).
//
// pages.ts still records each page's group as the sidebar drew it; this is
// the one place that says how the refreshed shell regroups them, so the rail
// on a computer and the bottom bar on a phone read the same list in the same
// order: Plan, Define, Verify, Agents, then the pages with no group.
//
// What moves: Overview and Summary leave Define for the head of Plan. They
// are where a project starts and where it stands, which is what Plan opens
// on.

/** The groups of the refreshed menu, in the order every shell draws them. */
export const REFRESH_GROUPS = ['Plan', 'Define', 'Verify', 'Agents'] as const;

/** The pages that move to the head of Plan, in the order they sit there. */
const PLAN_HEAD = ['', 'home'];

/**
 * Pages the refreshed menu no longer lists. Design showed the design items
 * Requirements already holds; its facts are in the artifact's pane there now,
 * and its address leads to Requirements filtered to design items.
 */
const DROPPED = ['design'];

/** The menu regrouped for the refreshed shell. Sections left empty are dropped. */
export const refreshSections = (sections: NavSection[]): NavSection[] => {
  const all = sections.flatMap((s) => s.items).filter((item) => !DROPPED.includes(item.to));
  const head = PLAN_HEAD.flatMap((to) => all.filter((item) => item.to === to));
  const rest = (items: NavItem[]) => items.filter((item) => !PLAN_HEAD.includes(item.to) && !DROPPED.includes(item.to));
  const grouped = REFRESH_GROUPS.map((label) => {
    const items = rest(sections.find((s) => s.label === label)?.items ?? []);
    return { label, items: label === 'Plan' ? [...head, ...items] : items };
  }).filter((s) => s.items.length > 0);
  const tail = sections.filter((s) => !s.label).map((s) => ({ items: rest(s.items) }));
  return [...grouped, ...tail.filter((s) => s.items.length > 0)];
};

/** The labelled group holding the page in view, if it is in one. */
export const activeGroup = (sections: NavSection[], activePath: string): NavSection | undefined =>
  sections.find((s) => s.label && sectionHasActive(s.items, activePath));

// A group tab on a phone opens the page last read in that group, so going
// from Verify to Define and back returns to the same page rather than to the
// group's first. One page per group, per person, kept across reloads.
const LAST_PAGE_KEY = 'openv-nav-last-page';

/** The page last read in each group, by group label. Unreadable storage is an empty map. */
export const loadLastPages = (): Record<string, string> => {
  try {
    const parsed = JSON.parse(localStorage.getItem(LAST_PAGE_KEY) || '{}');
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
    const out: Record<string, string> = {};
    for (const [group, to] of Object.entries(parsed)) {
      if (typeof to === 'string') out[group] = to;
    }
    return out;
  } catch {
    return {};
  }
};

/** Remember the page in view as its group's last. Storage failures are ignored. */
export const rememberGroupPage = (sections: NavSection[], activePath: string): void => {
  const group = activeGroup(sections, activePath);
  if (!group?.label) return;
  try {
    localStorage.setItem(LAST_PAGE_KEY, JSON.stringify({ ...loadLastPages(), [group.label]: activePath }));
  } catch {
    /* a preference that cannot be stored is not worth an error */
  }
};

/**
 * Where a group's tab leads: the page last read there if the menu still
 * lists it (a gate may have closed, a guided definition may have left the
 * menu), else the group's first page.
 */
export const groupLanding = (section: NavSection, last: Record<string, string>): string => {
  const remembered = section.label ? last[section.label] : undefined;
  if (remembered !== undefined && section.items.some((item) => item.to === remembered)) return remembered;
  return section.items[0]?.to ?? '';
};

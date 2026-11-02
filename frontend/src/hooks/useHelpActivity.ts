import { useEffect, useState } from 'react';

// What the reader is doing on the page, for the help panel: OpenD's activity
// signals (OpenD ADR 0018, the manual topic "Help for the field, button or
// pane in use"). With them the help panel opens on the manual's page for the
// section, button or tab in use rather than for the page as a whole; OpenD
// tries them most specific first, each with its parents, then the page.
//
// The app opts elements in with two attributes, and nothing else is read:
//
//   data-help-key   a section, field or control, or a group of them. Focus
//                   inside it makes it the focus; pressing a button, link,
//                   tab, menu item, checkbox or radio inside it makes it the
//                   action as well.
//   data-help-pane  a tab panel, pane or dialog, or the tab that shows it.
//                   Focus or a press inside it makes it the pane.
//
// The values are context keys from the manual (the resourceids in manual/,
// and docs/docs-maintenance.md rule 5: keys are an interface). Only those
// keys leave the page: never what was typed, element text, ids or selectors.
// Events inside an element marked data-help-ui (the help panel and the ?
// buttons) are ignored, so opening help keeps the reader's place. Focus
// moving to an element without a key clears the focus; the action and pane
// are kept until the page changes.

/** The activity signals the help API takes beside the page's path. */
export interface HelpActivity {
  focus?: string;
  action?: string;
  pane?: string;
}

/** The longest key OpenD reads; it ignores longer ones, and so do we. */
const MAX_KEY = 256;
const KEY = /^[A-Za-z0-9._-]+$/;

const validKey = (v: string | null | undefined): string | undefined =>
  v && v.length <= MAX_KEY && KEY.test(v) ? v : undefined;

/** Pressing one of these inside a data-help-key element is an action. */
const PRESSABLE =
  'button, a[href], [role="button"], [role="tab"], [role="menuitem"], [role="link"], ' +
  'input[type="checkbox"], input[type="radio"], input[type="button"], input[type="submit"]';

/** The nearest element on the event's path (shadow roots included) that
 *  carries attr, or undefined. */
const nearest = (path: EventTarget[], attr: string): Element | undefined =>
  path.find((t): t is Element => t instanceof Element && t.hasAttribute(attr));

const same = (a: HelpActivity, b: HelpActivity) =>
  a.focus === b.focus && a.action === b.action && a.pane === b.pane;

/** Only the signals that are set, so a request carries no empty values. */
export const activityParams = (a: HelpActivity): HelpActivity => {
  const out: HelpActivity = {};
  if (a.focus) out.focus = a.focus;
  if (a.action) out.action = a.action;
  if (a.pane) out.pane = a.pane;
  return out;
};

/**
 * Tracks the activity signals on the whole document while enabled, starting
 * afresh whenever page changes (pass the route's path). Disabled, it tracks
 * nothing and returns no signals.
 */
export const useHelpActivity = (enabled: boolean, page: string): HelpActivity => {
  const [activity, setActivity] = useState<HelpActivity>({});

  useEffect(() => {
    setActivity({});
    if (!enabled) return undefined;
    const update = (next: (prev: HelpActivity) => HelpActivity) =>
      setActivity((prev) => {
        const n = next(prev);
        return same(prev, n) ? prev : n;
      });

    const onFocus = (e: Event) => {
      const path = e.composedPath();
      if (nearest(path, 'data-help-ui')) return;
      const focus = validKey(nearest(path, 'data-help-key')?.getAttribute('data-help-key'));
      const pane = validKey(nearest(path, 'data-help-pane')?.getAttribute('data-help-pane'));
      update((prev) => ({ ...prev, focus, pane: pane ?? prev.pane }));
    };

    const onClick = (e: Event) => {
      const path = e.composedPath();
      if (nearest(path, 'data-help-ui')) return;
      const keyed = nearest(path, 'data-help-key');
      const pressed = path.find((t): t is Element => t instanceof Element && t.matches(PRESSABLE));
      // The path runs from the target outwards, so a press counts when the
      // control pressed is the keyed element or sits inside it.
      const action =
        keyed && pressed && path.indexOf(pressed) <= path.indexOf(keyed)
          ? validKey(keyed.getAttribute('data-help-key'))
          : undefined;
      const pane = validKey(nearest(path, 'data-help-pane')?.getAttribute('data-help-pane'));
      update((prev) => ({ ...prev, action: action ?? prev.action, pane: pane ?? prev.pane }));
    };

    document.addEventListener('focusin', onFocus, true);
    document.addEventListener('click', onClick, true);
    return () => {
      document.removeEventListener('focusin', onFocus, true);
      document.removeEventListener('click', onClick, true);
    };
  }, [enabled, page]);

  return activity;
};

import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { helpKeyForPath } from '../pages';
import { useAppStore } from '../state/store';
import { useFeature } from './useFeature';
import type { FeatureKey } from '../generated/contract';

// The help panel's state, shared by everything that opens it or tells it
// what the member is doing (docs/plans/opend-help.md, "one click from every
// screen"):
//
// - open: whether the panel is showing. Every shell's ? button, a HelpLink
//   and the ? / F1 keys drive the same state, so help opens from anywhere.
// - the context the panel shows, which is the first of:
//   1. the key a HelpLink asked for, until the panel closes, the page
//      changes or a new context arrives;
//   2. the innermost active context: a dialog over a tab or step over the
//      page, and among equals the one that became active last
//      (useHelpContext);
//   3. the page's own key, from pages.ts.
//   The panel follows it live: open a dialog with the panel open and it
//   switches to the dialog's topic; close the dialog and it returns.
//
// All of it is behind the manual-help feature and the help-docs capability
// (useDocsHelp): with either off, nothing here changes what a member sees.

/** The deployment capability that serves help from the docs site. */
export const CAPABILITY_HELP_DOCS = 'help-docs';

/** The release gate on it: the member's channel must have reached it too. */
export const MANUAL_HELP_FEATURE: FeatureKey = 'manual-help';

/** A context key, with the section (heading id) to scroll to inside its topic. */
export interface HelpTarget {
  key: string;
  fragment?: string;
}

interface Entry extends HelpTarget {
  id: number;
  /** 2 for a dialog, 1 for a tab, step or panel: the higher layer wins. */
  layer: number;
  /** When it became active; the later wins within a layer. */
  seq: number;
}

export interface HelpState {
  open: boolean;
  setOpen: React.Dispatch<React.SetStateAction<boolean>>;
  /** Opens the panel at a key, as a HelpLink does. */
  openAt: (key: string, fragment?: string) => void;
  /** Opens the panel at whatever the member is looking at now. */
  openCurrent: () => void;
  /** What the panel shows: a key, or '' when the page has none. */
  current: HelpTarget;
  /** Internal to useHelpContext: registers an active context. */
  register: (target: HelpTarget, layer: number) => () => void;
}

const HelpCtx = createContext<HelpState | null>(null);

/** Whether this member gets help from the manual: the gate and the capability. */
export const useDocsHelp = (): boolean => {
  const offered = useAppStore((s) => s.capabilities?.includes(CAPABILITY_HELP_DOCS) ?? false);
  const orgId = useAppStore((s) => s.activeOrgId);
  const gated = useFeature(MANUAL_HELP_FEATURE);
  return offered && gated && !!orgId;
};

/** Typing somewhere, where ? is a character and not a shortcut. */
const typing = (el: EventTarget | null): boolean => {
  if (!(el instanceof HTMLElement)) return false;
  return el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName);
};

export const HelpProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [open, setOpen] = useState(false);
  const [entries, setEntries] = useState<Entry[]>([]);
  const [asked, setAsked] = useState<HelpTarget | null>(null);
  const next = useRef(0);
  const { pathname } = useLocation();
  const pageKey = helpKeyForPath(pathname);
  const docsHelp = useDocsHelp();

  const register = useCallback((target: HelpTarget, layer: number) => {
    const id = ++next.current;
    setEntries((list) => [...list, { ...target, id, layer, seq: id }]);
    // A new context outranks a key a help link asked for.
    setAsked(null);
    return () => setEntries((list) => list.filter((e) => e.id !== id));
  }, []);

  // A help link's key lasts while the member stays where they clicked it.
  useEffect(() => setAsked(null), [pathname]);
  useEffect(() => {
    if (!open) setAsked(null);
  }, [open]);

  const top = useMemo(
    () =>
      entries.reduce<Entry | null>(
        (best, e) => (!best || e.layer > best.layer || (e.layer === best.layer && e.seq > best.seq) ? e : best),
        null,
      ),
    [entries],
  );
  const topKey = top?.key;
  const topFragment = top?.fragment;
  const current = useMemo<HelpTarget>(() => {
    if (asked) return asked;
    if (topKey) return { key: topKey, fragment: topFragment };
    return { key: pageKey };
  }, [asked, topKey, topFragment, pageKey]);

  const openAt = useCallback((key: string, fragment?: string) => {
    setAsked({ key, fragment });
    setOpen(true);
  }, []);
  const openCurrent = useCallback(() => {
    setAsked(null);
    setOpen(true);
  }, []);

  // ? (outside a field) and F1 open help for what is on screen.
  useEffect(() => {
    if (!docsHelp) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
      if (e.key === 'F1' || (e.key === '?' && !typing(e.target))) {
        e.preventDefault();
        openCurrent();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [docsHelp, openCurrent]);

  const value = useMemo<HelpState>(
    () => ({ open, setOpen, openAt, openCurrent, current, register }),
    [open, openAt, openCurrent, current, register],
  );
  return <HelpCtx.Provider value={value}>{children}</HelpCtx.Provider>;
};

/**
 * The help panel's state. Outside a HelpProvider (a component rendered on
 * its own in a test) it is a panel of its own with no contexts.
 */
export const useHelp = (): HelpState => {
  const shared = useContext(HelpCtx);
  const [open, setOpen] = useState(false);
  const own = useMemo<HelpState>(
    () => ({
      open,
      setOpen,
      openAt: () => setOpen(true),
      openCurrent: () => setOpen(true),
      current: { key: '' },
      register: () => () => undefined,
    }),
    [open],
  );
  return shared ?? own;
};

/**
 * Tells the help panel what the member is doing while the caller is mounted
 * and `key` is set: a tab, a wizard step, a panel, or (with `dialog`) a
 * dialog, which outranks the rest. Pass null to stand down, as a tab strip
 * does for a tab that has no topic of its own.
 */
export const useHelpContext = (
  key: string | null | undefined,
  options: { fragment?: string; dialog?: boolean } = {},
): void => {
  const { register } = useHelp();
  const { fragment, dialog } = options;
  useEffect(() => {
    if (!key) return;
    return register({ key, fragment }, dialog ? 2 : 1);
  }, [register, key, fragment, dialog]);
};

// The floating V&V Assistant's app-level state (#726): whether the panel is
// open, what the page beneath it is showing, and a turn another page asked
// it to start. A store rather than a React context, so that any page (or a
// plain function) can call openAssistant without being wrapped in a
// provider, and so the panel above the router keeps its state while the
// pages beneath it change.
import { useEffect } from 'react';
import { create } from 'zustand';
import type { FeatureKey } from '../../generated/contract';

/**
 * The floating assistant on every project page and the workspace Home, its
 * one conversation across pages, the V&V dashboard's "Draft tests", and the
 * artifact's History as a section of its pane (the Notes panel's retirement).
 */
export const FLOATING_ASSISTANT_FEATURE: FeatureKey = 'floating-assistant';

/** What the reader is looking at, as the panel's "Looking at:" line shows it. */
export interface AssistantContext {
  /** The line's text, e.g. "REQ-73 Change impact analysis" or "V&V dashboard". */
  label: string;
  /** The artifact on screen, sent with each turn so the assistant reads it. */
  artifactId?: string;
}

/** A turn a page asked the assistant to start, sent once the chat is ready. */
export interface AssistantRequest {
  id: number;
  message: string;
}

/** What a page registers about itself while it is mounted. */
export interface AssistantPageContext {
  /** The page's own context; the panel shows it until the reader removes it. */
  context?: AssistantContext;
}

export type AssistantPanelState = 'closed' | 'open' | 'expanded';

interface AssistantState {
  panel: AssistantPanelState;
  /** The page's registration, replaced as pages mount and unmount. */
  page: AssistantPageContext;
  /** A context a caller passed with openAssistant, which outranks the page's. */
  override: AssistantContext | null;
  /** The label of the context the reader removed, until the context changes. */
  dismissed: string | null;
  request: AssistantRequest | null;
  setPanel: (panel: AssistantPanelState) => void;
  setPage: (page: AssistantPageContext) => void;
  /** Stop showing and sending the context with this label, until it changes. */
  dismissContext: (label: string) => void;
  takeRequest: (id: number) => void;
}

let nextRequestId = 1;

export const useAssistantStore = create<AssistantState>((set) => ({
  panel: 'closed',
  page: {},
  override: null,
  dismissed: null,
  request: null,
  setPanel: (panel) => set({ panel }),
  setPage: (page) => set({ page, override: null, dismissed: null }),
  dismissContext: (label) => set({ dismissed: label }),
  takeRequest: (id) => set((s) => (s.request?.id === id ? { request: null } : {})),
}));

/**
 * Open the floating assistant, optionally starting a turn with `message` at
 * once and replacing the page's context with `context`. The turn waits until
 * the conversation has been opened, so calling this before the panel exists
 * is fine.
 */
export const openAssistant = (opts: { message?: string; context?: AssistantContext } = {}) => {
  const { panel } = useAssistantStore.getState();
  useAssistantStore.setState({
    panel: panel === 'closed' ? 'open' : panel,
    ...(opts.context ? { override: opts.context, dismissed: null } : {}),
    ...(opts.message?.trim() ? { request: { id: nextRequestId++, message: opts.message.trim() } } : {}),
  });
};

// The mounted page's refresh, kept outside the store: it is a fresh closure
// every render, and nothing should re-render because it changed.
let artifactsChanged: (() => void) | undefined;

/**
 * Tell the page beneath the panel that the assistant added something to the
 * project, so a suggestion applied from the floating panel shows without the
 * reader navigating to find it.
 */
export const notifyArtifactsChanged = () => artifactsChanged?.();

/**
 * Register what this page shows with the floating assistant while it is
 * mounted. The label and artifact are compared by value, so a page may pass
 * a fresh object every render; the latest refresh callback is always used.
 */
export const useAssistantPageContext = (
  context: AssistantContext | undefined,
  onArtifactsChanged?: () => void
) => {
  const label = context?.label;
  const artifactId = context?.artifactId;
  const setPage = useAssistantStore((s) => s.setPage);
  artifactsChanged = onArtifactsChanged;
  useEffect(() => {
    setPage({ context: label ? { label, artifactId } : undefined });
  }, [label, artifactId, setPage]);
  useEffect(
    () => () => {
      setPage({});
      artifactsChanged = undefined;
    },
    [setPage]
  );
};

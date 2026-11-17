// The floating assistant's size, corner and keyboard shortcut (#726).
import React, { useCallback, useEffect, useRef } from 'react';

export interface PanelLayout {
  width: number;
  height: number;
  /** The corner it sits in: the other one when it would cover a page's controls. */
  side: 'left' | 'right';
}

/** Per browser: a convenience, so a lost value costs only the default. */
const STORAGE_KEY = 'openv-assistant-panel';
export const DEFAULT_LAYOUT: PanelLayout = { width: 400, height: 560, side: 'right' };
const MIN = { width: 300, height: 320 };

const clamp = (layout: PanelLayout): PanelLayout => ({
  width: Math.max(MIN.width, Math.round(layout.width)),
  height: Math.max(MIN.height, Math.round(layout.height)),
  side: layout.side === 'left' ? 'left' : 'right',
});

export const loadPanelLayout = (): PanelLayout => {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULT_LAYOUT;
    const parsed = JSON.parse(raw);
    if (typeof parsed?.width !== 'number' || typeof parsed?.height !== 'number') return DEFAULT_LAYOUT;
    return clamp(parsed);
  } catch {
    return DEFAULT_LAYOUT;
  }
};

export const savePanelLayout = (layout: PanelLayout): void => {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(clamp(layout)));
  } catch {
    // storage blocked: the size lasts for the visit
  }
};

/**
 * Alt+A, or Option+A on a Mac. Read from the key's position (code) rather
 * than its character, since Option+A types "å"; with Ctrl, Meta or Shift
 * it belongs to something else.
 */
export const isAssistantShortcut = (e: {
  code?: string;
  key?: string;
  altKey?: boolean;
  ctrlKey?: boolean;
  metaKey?: boolean;
  shiftKey?: boolean;
  defaultPrevented?: boolean;
}): boolean =>
  !e.defaultPrevented &&
  !!e.altKey &&
  !e.ctrlKey &&
  !e.metaKey &&
  !e.shiftKey &&
  (e.code === 'KeyA' || e.key === 'a' || e.key === 'A');

/**
 * Drag the panel's top inner corner to resize it. The panel is anchored at
 * the bottom and at its side, so dragging up and inwards makes it bigger.
 * The size is reported when the drag ends.
 */
export const useResizeFromCorner = (layout: PanelLayout, onDone: (layout: PanelLayout) => void) => {
  const drag = useRef<{ x: number; y: number; start: PanelLayout; last: PanelLayout } | null>(null);
  const layoutRef = useRef(layout);
  layoutRef.current = layout;
  const doneRef = useRef(onDone);
  doneRef.current = onDone;
  const frameTarget = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const move = (e: MouseEvent) => {
      const d = drag.current;
      if (!d) return;
      const dx = e.clientX - d.x;
      const dy = e.clientY - d.y;
      const next = clamp({
        side: d.start.side,
        width: d.start.width + (d.start.side === 'right' ? -dx : dx),
        height: d.start.height - dy,
      });
      d.last = next;
      const el = frameTarget.current;
      if (el) {
        el.style.width = `${next.width}px`;
        el.style.height = `${next.height}px`;
      }
    };
    const up = () => {
      const d = drag.current;
      if (!d) return;
      drag.current = null;
      doneRef.current(d.last);
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    return () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
    };
  }, []);

  return useCallback((e: React.MouseEvent<HTMLElement>) => {
    e.preventDefault();
    frameTarget.current = e.currentTarget.parentElement;
    drag.current = { x: e.clientX, y: e.clientY, start: layoutRef.current, last: layoutRef.current };
  }, []);
};

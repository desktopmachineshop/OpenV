import { useCallback, useEffect, useRef, useState } from 'react';
import type { PointerEvent as ReactPointerEvent } from 'react';

// Panning and zooming a diagram inside a fixed viewport: drag the background
// to pan, the wheel (or a pinch) to zoom about the pointer, buttons for in,
// out and fit, and a call that brings one box into view. The transform is
// translate(x, y) scale(k) on the content, from its top-left corner.

export interface View {
  x: number;
  y: number;
  k: number;
}

export const MIN_ZOOM = 0.2;
export const MAX_ZOOM = 2;
const clampK = (k: number) => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, k));

/** The view that fits a width × height into a viewport, never above 1:1. */
export const fitView = (vw: number, vh: number, width: number, height: number, margin = 16): View => {
  if (!vw || !vh || !width || !height) return { x: 0, y: 0, k: 1 };
  const k = clampK(Math.min((vw - margin * 2) / width, (vh - margin * 2) / height, 1));
  return { x: (vw - width * k) / 2, y: Math.max(margin, (vh - height * k) / 2), k };
};

/** Zooms by `factor` keeping the viewport point (px, py) where it is. */
export const zoomAbout = (v: View, factor: number, px: number, py: number): View => {
  const k = clampK(v.k * factor);
  const f = k / v.k;
  return { k, x: px - (px - v.x) * f, y: py - (py - v.y) * f };
};

/** The smallest pan that brings a content rectangle into the viewport, or the view unchanged. */
export const revealRect = (v: View, vw: number, vh: number, r: { x: number; y: number; w: number; h: number }, margin = 24): View => {
  if (!vw || !vh) return v;
  const left = v.x + r.x * v.k;
  const top = v.y + r.y * v.k;
  const right = left + r.w * v.k;
  const bottom = top + r.h * v.k;
  let dx = 0;
  let dy = 0;
  if (left < margin) dx = margin - left;
  else if (right > vw - margin) dx = Math.max(margin - left, vw - margin - right);
  if (top < margin) dy = margin - top;
  else if (bottom > vh - margin) dy = Math.max(margin - top, vh - margin - bottom);
  return dx || dy ? { ...v, x: v.x + dx, y: v.y + dy } : v;
};

export function usePanZoom(width: number, height: number) {
  const viewport = useRef<HTMLDivElement>(null);
  const [view, setView] = useState<View>({ x: 0, y: 0, k: 1 });
  const [size, setSize] = useState({ w: 0, h: 0 });
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const pinch = useRef<number | null>(null);

  useEffect(() => {
    const el = viewport.current;
    if (!el) return;
    const measure = () => setSize({ w: el.clientWidth, h: el.clientHeight });
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // React registers wheel listeners as passive, which cannot stop the page
  // scrolling; this one is registered by hand so it can.
  useEffect(() => {
    const el = viewport.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const box = el.getBoundingClientRect();
      const factor = Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.0015));
      setView((v) => zoomAbout(v, factor, e.clientX - box.left, e.clientY - box.top));
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);

  const onPointerDown = (e: ReactPointerEvent) => {
    // A press on a box or a button is a click, not a pan.
    if ((e.target as HTMLElement).closest('[role="treeitem"], button')) return;
    // Drop a selection a click elsewhere left, so the pan drags nothing.
    window.getSelection?.()?.removeAllRanges();
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
  };
  const onPointerMove = (e: ReactPointerEvent) => {
    const prev = pointers.current.get(e.pointerId);
    if (!prev) return;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.current.size === 2) {
      const [a, b] = Array.from(pointers.current.values());
      const dist = Math.hypot(a.x - b.x, a.y - b.y);
      const box = viewport.current?.getBoundingClientRect();
      if (pinch.current && box) {
        const factor = dist / pinch.current;
        setView((v) => zoomAbout(v, factor, (a.x + b.x) / 2 - box.left, (a.y + b.y) / 2 - box.top));
      }
      pinch.current = dist;
      return;
    }
    setView((v) => ({ ...v, x: v.x + e.clientX - prev.x, y: v.y + e.clientY - prev.y }));
  };
  const onPointerUp = (e: ReactPointerEvent) => {
    pointers.current.delete(e.pointerId);
    if (pointers.current.size < 2) pinch.current = null;
  };

  const fit = useCallback(() => setView(fitView(size.w, size.h, width, height)), [size.w, size.h, width, height]);
  const zoom = useCallback((factor: number) => setView((v) => zoomAbout(v, factor, size.w / 2, size.h / 2)), [size.w, size.h]);
  const reveal = useCallback(
    (r: { x: number; y: number; w: number; h: number }) => setView((v) => revealRect(v, size.w, size.h, r)),
    [size.w, size.h]
  );

  return {
    viewport,
    view,
    size,
    fit,
    zoom,
    reveal,
    handlers: { onPointerDown, onPointerMove, onPointerUp, onPointerCancel: onPointerUp },
  };
}

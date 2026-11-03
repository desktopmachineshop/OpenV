import React, { useEffect, useState } from 'react';

export interface ColumnResizeOptions {
  /** The window's width, which the detail column yields to. */
  viewportWidth: number;
  /** Whether the notes column takes width beside the document (pinned). */
  notesTakesSpace: boolean;
}

/** The narrowest the list beside the detail panel is drawn. */
export const LIST_MIN = 420;

/**
 * The module's two resizable columns: the detail panel (the selected
 * artifact's document) to the right of the list, and the notes on the far
 * right. Each width is remembered (openv-detailColumnWidth,
 * openv-rightColumnWidth) and changed by dragging the handle on its left
 * edge; the width is saved when the drag ends. The detail panel is drawn
 * narrower than its saved width when the window cannot give the list its
 * room.
 */
export function useColumnResize({ viewportWidth, notesTakesSpace }: ColumnResizeOptions) {
  const [leftColumnWidth, setLeftColumnWidth] = useState<number>(() => {
    const saved = localStorage.getItem('openv-detailColumnWidth');
    return saved ? parseInt(saved) : 460;
  });
  const [rightColumnWidth, setRightColumnWidth] = useState<number>(() => {
    const saved = localStorage.getItem('openv-rightColumnWidth');
    return saved ? parseInt(saved) : 320;
  });
  const [isResizing, setIsResizing] = useState<'left' | 'right' | null>(null);
  // The detail column as drawn: the saved width, clamped so the list keeps at
  // least LIST_MIN beside the project rail and a pinned notes column. The
  // saved value is untouched: a wider window gets it back.
  const leftColumnDrawn = Math.max(
    340,
    Math.min(leftColumnWidth, viewportWidth - 104 - (notesTakesSpace ? rightColumnWidth + 10 : 24) - LIST_MIN)
  );
  // Drag origin for a column resize: the pointer position and column width at
  // mousedown. Resizing is a delta from that origin rather than an absolute
  // position derived from clientX, so the divider stays under the cursor no
  // matter what sits to the left of the columns (the 200px project sidebar,
  // page padding, the handle itself) instead of snapping on grab (issue: the
  // divider jumped ~a sidebar's width left on the first mouse move).
  const resizeOrigin = React.useRef<{ side: 'left' | 'right'; startX: number; startWidth: number } | null>(null);
  // Latest width during a drag, so mouseup can persist it without the effect
  // having to re-subscribe on every mousemove.
  const liveWidths = React.useRef({ left: leftColumnWidth, right: rightColumnWidth });

  // Start a column resize: record where the drag began so the move handler can
  // work in deltas. preventDefault keeps the browser from starting a text
  // selection or native drag under the cursor.
  const startResize = (side: 'left' | 'right') => (e: React.MouseEvent) => {
    e.preventDefault();
    resizeOrigin.current = {
      side,
      startX: e.clientX,
      startWidth: side === 'left' ? leftColumnDrawn : rightColumnWidth,
    };
    setIsResizing(side);
  };

  // Handle column resizing. Depends only on isResizing: the handlers read the
  // in-flight width from a ref, so the listeners are attached once per drag
  // rather than being torn down and re-added on every mousemove.
  useEffect(() => {
    if (!isResizing) return;

    const handleMouseMove = (e: MouseEvent) => {
      const origin = resizeOrigin.current;
      if (!origin) return;

      // Both columns sit right of their handles, so both grow as the
      // pointer moves left.
      const delta = origin.startX - e.clientX;
      const [min, max] = origin.side === 'left' ? [340, 900] : [250, 600];
      const newWidth = Math.max(min, Math.min(max, origin.startWidth + delta));

      liveWidths.current[origin.side] = newWidth;
      if (origin.side === 'left') {
        setLeftColumnWidth(newWidth);
      } else {
        setRightColumnWidth(newWidth);
      }
    };

    const handleMouseUp = () => {
      const origin = resizeOrigin.current;
      if (origin?.side === 'right') {
        localStorage.setItem('openv-rightColumnWidth', liveWidths.current.right.toString());
      } else if (origin?.side === 'left') {
        localStorage.setItem('openv-detailColumnWidth', liveWidths.current.left.toString());
      }
      resizeOrigin.current = null;
      setIsResizing(null);
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, [isResizing]);

  return { rightColumnWidth, isResizing, leftColumnDrawn, startResize };
}

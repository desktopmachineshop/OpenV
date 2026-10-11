import React, { useEffect, useState } from 'react';

export interface ColumnResizeOptions {
  /** The window's width, which the tree column yields to. */
  viewportWidth: number;
  /** Whether the notes column takes width beside the document (pinned). */
  notesTakesSpace: boolean;
}

/**
 * The module's two resizable columns: the tree on the left and the notes on
 * the right. Each width is remembered (openv-leftColumnWidth,
 * openv-rightColumnWidth) and changed by dragging the handle beside its
 * column; the width is saved when the drag ends. The tree is drawn narrower
 * than its saved width when the window cannot give the document its room.
 */
export function useColumnResize({ viewportWidth, notesTakesSpace }: ColumnResizeOptions) {
  // Resizable columns state
  const [leftColumnWidth, setLeftColumnWidth] = useState<number>(() => {
    const saved = localStorage.getItem('openv-leftColumnWidth');
    return saved ? parseInt(saved) : 400;
  });
  const [rightColumnWidth, setRightColumnWidth] = useState<number>(() => {
    const saved = localStorage.getItem('openv-rightColumnWidth');
    return saved ? parseInt(saved) : 320;
  });
  const [isResizing, setIsResizing] = useState<'left' | 'right' | null>(null);
  // The tree column as drawn: the saved width, clamped so the document keeps
  // at least 420px beside the project sidebar and a pinned notes column. The
  // saved value is untouched — a wider window gets it back.
  const leftColumnDrawn = Math.max(
    200,
    Math.min(leftColumnWidth, viewportWidth - 260 - (notesTakesSpace ? rightColumnWidth + 10 : 10) - 420)
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

      // The left column grows as the pointer moves right; the right column
      // grows as it moves left.
      const delta = origin.side === 'left' ? e.clientX - origin.startX : origin.startX - e.clientX;
      const [min, max] = origin.side === 'left' ? [200, 800] : [250, 600];
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
        localStorage.setItem('openv-leftColumnWidth', liveWidths.current.left.toString());
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

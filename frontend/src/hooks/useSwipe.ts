import { useCallback, useRef } from 'react';
import { SwipeDirection, startsNearEdge, swipeDirection } from './swipe';
import { overlayIsOpen } from './readingKeys';

/** Handlers to spread onto the element a swipe is read from. */
export interface SwipeHandlers {
  onTouchStart: (event: React.TouchEvent) => void;
  onTouchMove: (event: React.TouchEvent) => void;
  onTouchEnd: (event: React.TouchEvent) => void;
  onTouchCancel: () => void;
}

/**
 * Whether the gesture began inside something that scrolls sideways on its own
 * — a wide markdown table, a code block, the figure strip.
 *
 * Those are the places where a sideways flick already means something, and
 * stealing it would leave the content unreachable. The element's own scrolling
 * wins, so the gesture is dropped at its start rather than judged at its end.
 */
const startsInSideScroller = (target: EventTarget | null, root: Element | null): boolean => {
  let node = target instanceof Element ? target : null;
  while (node && node !== root) {
    if (node.scrollWidth > node.clientWidth + 1) {
      const overflowX = window.getComputedStyle(node).overflowX;
      if (overflowX === 'auto' || overflowX === 'scroll') return true;
    }
    node = node.parentElement;
  }
  return false;
};

/**
 * A horizontal swipe on one element, reported once the finger lifts.
 *
 * Nothing is ever preventDefault-ed: the pane this sits on scrolls vertically,
 * and a gesture that turns out to be a scroll must scroll normally, so the
 * decision waits for touchend and the browser keeps the whole gesture. A
 * second finger, a gesture starting in a sideways scroller, or travel that
 * fails swipeDirection's test all end in nothing happening.
 */
export const useHorizontalSwipe = (
  enabled: boolean,
  onSwipe: (direction: SwipeDirection) => void
): SwipeHandlers => {
  const gesture = useRef<{ x: number; y: number; at: number; live: boolean } | null>(null);

  const onTouchStart = useCallback(
    (event: React.TouchEvent) => {
      if (!enabled || event.touches.length !== 1) {
        gesture.current = null;
        return;
      }
      const touch = event.touches[0];
      gesture.current = {
        x: touch.clientX,
        y: touch.clientY,
        at: Date.now(),
        live:
          !overlayIsOpen() &&
          !startsNearEdge(touch.clientX, window.innerWidth) &&
          !startsInSideScroller(event.target, event.currentTarget),
      };
    },
    [enabled]
  );

  // A pinch or a second finger is not a swipe, and the gesture cannot recover:
  // the travel from here on belongs to a zoom.
  const onTouchMove = useCallback((event: React.TouchEvent) => {
    if (event.touches.length > 1 && gesture.current) gesture.current.live = false;
  }, []);

  const onTouchEnd = useCallback(
    (event: React.TouchEvent) => {
      const start = gesture.current;
      gesture.current = null;
      if (!enabled || !start?.live) return;
      const touch = event.changedTouches[0];
      if (!touch) return;
      const direction = swipeDirection({
        dx: touch.clientX - start.x,
        dy: touch.clientY - start.y,
        ms: Date.now() - start.at,
      });
      if (direction) onSwipe(direction);
    },
    [enabled, onSwipe]
  );

  const onTouchCancel = useCallback(() => {
    gesture.current = null;
  }, []);

  return { onTouchStart, onTouchMove, onTouchEnd, onTouchCancel };
};

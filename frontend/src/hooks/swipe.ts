// When a finger's travel counts as a horizontal swipe.
//
// The numbers live here, away from the DOM, because what makes a swipe a swipe
// is a judgement rather than an event: far enough to be deliberate, straight
// enough not to be a scroll, and quick enough not to be a drag. Pure, so the
// judgement is tested without a touchscreen.

/** Travel below this is a tap, or a finger that slipped. */
export const SWIPE_MIN_DISTANCE = 60;

/**
 * How much the horizontal travel must beat the vertical. A reader scrolling a
 * requirement with their thumb drifts sideways as they go; at 1.5 that drift
 * has to become the larger half of the gesture before it is read as a swipe.
 */
export const SWIPE_DOMINANCE = 1.5;

/**
 * Longer than this is not a flick. A slow sideways drag is someone scrolling,
 * holding still, or hesitating over a link, and none of those mean "next".
 */
export const SWIPE_MAX_DURATION_MS = 800;

/**
 * A gesture starting this close to either edge belongs to the browser: Safari
 * on iOS and Chrome on Android both read an edge swipe as back or forward. One
 * gesture must not both leave the page and change the artifact on it.
 */
export const SWIPE_EDGE_GUARD = 24;

/** Whether a touch began in the browser's own edge-gesture strip. */
export const startsNearEdge = (x: number, viewportWidth: number): boolean =>
  x <= SWIPE_EDGE_GUARD || x >= viewportWidth - SWIPE_EDGE_GUARD;

export type SwipeDirection = 'next' | 'previous';

export interface Travel {
  dx: number;
  dy: number;
  ms: number;
}

/**
 * The step a gesture asks for, or null when it asks for nothing.
 *
 * Swiping left — the content pulled leftwards, the way a page turns — means
 * next, which is the direction every photo viewer and e-reader already
 * teaches.
 */
export const swipeDirection = ({ dx, dy, ms }: Travel): SwipeDirection | null => {
  if (ms > SWIPE_MAX_DURATION_MS) return null;
  const across = Math.abs(dx);
  if (across < SWIPE_MIN_DISTANCE) return null;
  if (across < Math.abs(dy) * SWIPE_DOMINANCE) return null;
  return dx < 0 ? 'next' : 'previous';
};

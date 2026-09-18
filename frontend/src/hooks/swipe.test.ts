import {
  SWIPE_MAX_DURATION_MS,
  SWIPE_MIN_DISTANCE,
  swipeDirection,
} from './swipe';

// A swipe has to be told apart from the two gestures it sits between: a tap
// that slipped, and a thumb scrolling the requirement. These are the rules
// that decide it.

describe('swipeDirection', () => {
  const quick = { ms: 200 };

  it('reads a leftward flick as next, the way a page turns', () => {
    expect(swipeDirection({ dx: -120, dy: 0, ...quick })).toBe('next');
  });

  it('reads a rightward flick as previous', () => {
    expect(swipeDirection({ dx: 120, dy: 0, ...quick })).toBe('previous');
  });

  it('ignores travel too short to be deliberate', () => {
    expect(swipeDirection({ dx: -(SWIPE_MIN_DISTANCE - 1), dy: 0, ...quick })).toBeNull();
  });

  it('takes travel exactly at the threshold', () => {
    expect(swipeDirection({ dx: -SWIPE_MIN_DISTANCE, dy: 0, ...quick })).toBe('next');
  });

  it('ignores a thumb that was mostly scrolling', () => {
    expect(swipeDirection({ dx: -100, dy: 200, ...quick })).toBeNull();
  });

  it('allows the sideways drift of a real flick', () => {
    expect(swipeDirection({ dx: -150, dy: 40, ...quick })).toBe('next');
  });

  it('ignores a slow drag, however far it went', () => {
    expect(swipeDirection({ dx: -400, dy: 0, ms: SWIPE_MAX_DURATION_MS + 1 })).toBeNull();
  });

  it('ignores a finger that did not move', () => {
    expect(swipeDirection({ dx: 0, dy: 0, ms: 50 })).toBeNull();
  });
});

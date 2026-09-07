import { useEffect, useState } from 'react';
import { Viewport, describeViewport } from './viewport';

const coarsePointerQuery = '(hover: none) and (pointer: coarse)';

const read = (): Viewport => {
  if (typeof window === 'undefined') return describeViewport(1280, false);
  const coarse =
    typeof window.matchMedia === 'function' ? window.matchMedia(coarsePointerQuery).matches : false;
  return describeViewport(window.innerWidth, coarse);
};

/**
 * The viewport class the layout should follow, kept current across resizes
 * and orientation changes. Read once per render tree in the components that
 * change shape (the shell, the requirements module, the wizard); everything
 * below them takes the decision as a prop or a class name rather than
 * re-measuring.
 */
export const useViewport = (): Viewport => {
  const [viewport, setViewport] = useState<Viewport>(read);
  useEffect(() => {
    let frame = 0;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const next = read();
        setViewport((prev) =>
          prev.width === next.width && prev.coarsePointer === next.coarsePointer ? prev : next
        );
      });
    };
    window.addEventListener('resize', update);
    window.addEventListener('orientationchange', update);
    const mq = typeof window.matchMedia === 'function' ? window.matchMedia(coarsePointerQuery) : null;
    mq?.addEventListener?.('change', update);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('resize', update);
      window.removeEventListener('orientationchange', update);
      mq?.removeEventListener?.('change', update);
    };
  }, []);
  return viewport;
};

import { useLayoutEffect, type RefObject } from 'react';

/**
 * Sizes an element to run from where it starts down to the bottom of the
 * window, so a page made of panels fits the window and each panel scrolls on
 * its own instead of the whole page. It sets `--fit-top` on the element (its
 * distance from the top of the document) and the stylesheet does the rest:
 * `height: calc(100dvh - var(--fit-top))`. The distance is measured again
 * when the window or anything above the element changes size.
 */
export function useFitWindow(ref: RefObject<HTMLElement | null>, enabled: boolean): void {
  useLayoutEffect(() => {
    const el = ref.current;
    if (!enabled || !el) return undefined;
    const measure = () => {
      const top = Math.max(0, Math.round(el.getBoundingClientRect().top + window.scrollY));
      el.style.setProperty('--fit-top', `${top}px`);
    };
    measure();
    window.addEventListener('resize', measure);
    // A banner or bar above the element changes where it starts.
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure);
    observer?.observe(document.body);
    return () => {
      window.removeEventListener('resize', measure);
      observer?.disconnect();
      el.style.removeProperty('--fit-top');
    };
  }, [ref, enabled]);
}

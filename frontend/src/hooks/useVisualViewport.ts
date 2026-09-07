import { useEffect, useState } from 'react';

export interface VisualViewportBox {
  /** Height of the part of the page the person can actually see. */
  height: number;
  /** Space below the visual viewport taken by the on-screen keyboard. */
  keyboardInset: number;
}

const read = (): VisualViewportBox => {
  if (typeof window === 'undefined') return { height: 800, keyboardInset: 0 };
  const vv = window.visualViewport;
  if (!vv) return { height: window.innerHeight, keyboardInset: 0 };
  return {
    height: Math.round(vv.height),
    keyboardInset: Math.max(0, Math.round(window.innerHeight - vv.height - vv.offsetTop)),
  };
};

/**
 * The visual viewport: what remains of the screen once the on-screen
 * keyboard is up. Fixed elements anchor to the layout viewport, which iOS
 * never shrinks for the keyboard (and Android only with the
 * interactive-widget viewport setting), so a bottom sheet that must stay
 * above the keyboard sizes and offsets itself from this instead.
 */
export const useVisualViewport = (): VisualViewportBox => {
  const [box, setBox] = useState<VisualViewportBox>(read);
  useEffect(() => {
    const vv = window.visualViewport;
    const update = () => {
      const next = read();
      setBox((prev) => (prev.height === next.height && prev.keyboardInset === next.keyboardInset ? prev : next));
    };
    window.addEventListener('resize', update);
    vv?.addEventListener('resize', update);
    vv?.addEventListener('scroll', update);
    return () => {
      window.removeEventListener('resize', update);
      vv?.removeEventListener('resize', update);
      vv?.removeEventListener('scroll', update);
    };
  }, []);
  return box;
};

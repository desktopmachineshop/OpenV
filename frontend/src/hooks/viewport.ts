// Viewport classes for layout decisions (docs/plans/mobile-support.md).
//
// Two breakpoints, chosen from the devices the plan targets rather than from
// a framework: a phone in portrait is at most ~430 CSS px wide and a small
// tablet in portrait ~768, so anything up to 640 is laid out for one hand and
// one column, anything up to 900 loses the side panels but keeps a two-pane
// document, and above that the desktop layout applies unchanged. The numbers
// live here so components and tests share them.
export type ViewportClass = 'phone' | 'tablet' | 'desktop';

export const PHONE_MAX_WIDTH = 640;
export const TABLET_MAX_WIDTH = 900;

export const classifyViewport = (width: number): ViewportClass =>
  width <= PHONE_MAX_WIDTH ? 'phone' : width <= TABLET_MAX_WIDTH ? 'tablet' : 'desktop';

export interface Viewport {
  width: number;
  cls: ViewportClass;
  /** Up to and including the phone breakpoint: one column, drawers, sheets. */
  isPhone: boolean;
  /** Phone or tablet: side panels become drawers, the shell shows a top bar. */
  isCompact: boolean;
  /** The primary pointer cannot hover (touch): no hover-revealed panels. */
  coarsePointer: boolean;
}

export const describeViewport = (width: number, coarsePointer: boolean): Viewport => {
  const cls = classifyViewport(width);
  return {
    width,
    cls,
    isPhone: cls === 'phone',
    isCompact: cls !== 'desktop',
    coarsePointer,
  };
};

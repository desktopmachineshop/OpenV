import { PHONE_MAX_WIDTH, TABLET_MAX_WIDTH, classifyViewport, describeViewport } from './viewport';

describe('viewport classes', () => {
  it('classifies phones, tablets and desktops at the shared breakpoints', () => {
    expect(classifyViewport(320)).toBe('phone');
    expect(classifyViewport(PHONE_MAX_WIDTH)).toBe('phone');
    expect(classifyViewport(PHONE_MAX_WIDTH + 1)).toBe('tablet');
    expect(classifyViewport(TABLET_MAX_WIDTH)).toBe('tablet');
    expect(classifyViewport(TABLET_MAX_WIDTH + 1)).toBe('desktop');
    expect(classifyViewport(1440)).toBe('desktop');
  });

  it('derives the layout flags from the class', () => {
    expect(describeViewport(390, true)).toMatchObject({ isPhone: true, isCompact: true, coarsePointer: true });
    expect(describeViewport(768, true)).toMatchObject({ isPhone: false, isCompact: true });
    expect(describeViewport(1280, false)).toMatchObject({ isPhone: false, isCompact: false, coarsePointer: false });
  });
});

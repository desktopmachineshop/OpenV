import {
  activeNavPath,
  loadNavSections,
  navSectionIsOpen,
  saveNavSections,
  sectionHasActive,
  toggleNavSection,
} from './navSections';

describe('navSectionIsOpen', () => {
  it('collapses a section nobody has chosen', () => {
    expect(navSectionIsOpen({}, 'Define')).toBe(false);
  });

  it('opens the section holding the page being viewed', () => {
    expect(navSectionIsOpen({}, 'Define', true)).toBe(true);
  });

  it('lets an explicit choice beat both defaults', () => {
    expect(navSectionIsOpen({ Define: false }, 'Define', true)).toBe(false);
    expect(navSectionIsOpen({ Verify: true }, 'Verify', false)).toBe(true);
  });
});

describe('toggleNavSection', () => {
  it('closes a section that opened itself, in one click', () => {
    const next = toggleNavSection({}, 'Define', true);
    expect(navSectionIsOpen(next, 'Define', true)).toBe(false);
  });

  it('opens a collapsed section and leaves the others alone', () => {
    const next = toggleNavSection({ Verify: true }, 'Plan', false);
    expect(next).toEqual({ Verify: true, Plan: true });
  });
});

describe('activeNavPath', () => {
  it('reads the module out of a project route', () => {
    expect(activeNavPath('/projects/p1/requirements', 'p1')).toBe('requirements');
    expect(activeNavPath('/projects/p1/requirements/REQ-1', 'p1')).toBe('requirements');
    expect(activeNavPath('/projects/p1/', 'p1')).toBe('');
    expect(activeNavPath('/projects/p1', 'p1')).toBe('');
  });

  it('reports nothing outside the project', () => {
    expect(activeNavPath('/projects', 'p1')).toBe('');
    expect(activeNavPath('/projects/other/vv', 'p1')).toBe('');
    expect(activeNavPath('/projects/p1/vv', '')).toBe('');
  });
});

describe('sectionHasActive', () => {
  const define = [{ to: '' }, { to: 'guided' }, { to: 'requirements' }];

  it('finds the active item, overview included', () => {
    expect(sectionHasActive(define, 'requirements')).toBe(true);
    expect(sectionHasActive(define, '')).toBe(true);
  });

  it('says no for a section the reader is not in', () => {
    expect(sectionHasActive([{ to: 'vv' }, { to: 'matrix' }], 'requirements')).toBe(false);
  });
});

describe('stored state', () => {
  beforeEach(() => localStorage.clear());

  it('round-trips a choice', () => {
    saveNavSections({ Define: true, Plan: false });
    expect(loadNavSections()).toEqual({ Define: true, Plan: false });
  });

  it('ignores stored junk rather than breaking the menu', () => {
    localStorage.setItem('openv-nav-sections', 'not json');
    expect(loadNavSections()).toEqual({});
    localStorage.setItem('openv-nav-sections', '["Define"]');
    expect(loadNavSections()).toEqual({});
    localStorage.setItem('openv-nav-sections', '{"Define":"yes","Plan":true}');
    expect(loadNavSections()).toEqual({ Plan: true });
  });
});

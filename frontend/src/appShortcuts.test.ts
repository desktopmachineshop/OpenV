import {
  LAST_PROJECT_KEY,
  lastProject,
  projectPathFor,
  rememberProject,
  shortcutLabel,
  shortcutTarget,
} from './appShortcuts';

describe('shortcutTarget', () => {
  it('accepts the two project tabs the manifest shortcuts name', () => {
    expect(shortcutTarget('review')).toBe('review');
    expect(shortcutTarget('board')).toBe('board');
  });

  it('rejects anything else, so a hand-edited url cannot build a route', () => {
    for (const raw of [null, undefined, '', 'settings', '../../etc', 'REVIEW']) {
      expect(shortcutTarget(raw)).toBeNull();
    }
  });
});

describe('projectPathFor', () => {
  it('routes to the named tab, or the overview without one', () => {
    expect(projectPathFor('p1', 'review')).toBe('/projects/p1/review');
    expect(projectPathFor('p1', 'board')).toBe('/projects/p1/board');
    expect(projectPathFor('p1', null)).toBe('/projects/p1');
  });
});

describe('shortcutLabel', () => {
  it('names the target in prose for the "choose a project" banner', () => {
    expect(shortcutLabel('review')).toBe('review queue');
    expect(shortcutLabel('board')).toBe('board');
  });
});

describe('remembering the last project', () => {
  beforeEach(() => window.localStorage.clear());

  it('round-trips through localStorage', () => {
    expect(lastProject()).toBe('');
    rememberProject('p1');
    expect(window.localStorage.getItem(LAST_PROJECT_KEY)).toBe('p1');
    expect(lastProject()).toBe('p1');
  });

  it('survives storage that throws (private mode, blocked site data)', () => {
    const getItem = jest.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    const setItem = jest.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });

    expect(() => rememberProject('p1')).not.toThrow();
    expect(lastProject()).toBe('');

    getItem.mockRestore();
    setItem.mockRestore();
  });
});

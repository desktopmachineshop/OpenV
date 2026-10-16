import { noteHref, releaseTitle } from '../Changelog';
import { RELEASES, isNumbered, releaseAnchor } from '.';

// The public changelog's data (releases.json, generated from
// RELEASE_NOTES.md; CI's `release_notes.py site --check` keeps the two in
// step) and the small rules the page applies to it.

describe('public changelog', () => {
  it('holds released history only, newest first, every release with notes', () => {
    expect(RELEASES.length).toBeGreaterThan(0);
    for (const r of RELEASES) {
      expect(r.version).not.toBe('Unreleased');
      expect(r.date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
      expect(r.groups.flatMap((g) => g.notes).length).toBeGreaterThan(0);
    }
    const dates = RELEASES.map((r) => r.date);
    expect([...dates].sort().reverse()).toEqual(dates);
  });

  it('gives every release its own anchor', () => {
    const anchors = RELEASES.map(releaseAnchor);
    expect(new Set(anchors).size).toBe(anchors.length);
    for (const a of anchors) expect(a).toMatch(/^[uv][0-9-]+$/);
  });

  it('heads a numbered release by version and an early one by its day', () => {
    const numbered = RELEASES.find(isNumbered);
    expect(numbered && releaseTitle(numbered)).toBe(`OpenV ${numbered?.version}`);
    expect(releaseTitle({ version: '2026-09-12.2', date: '2026-09-12', stableSince: '', groups: [] })).toBe(
      'Update of 12 September 2026',
    );
  });

  it('points a note’s repository paths at GitHub and leaves real links alone', () => {
    expect(noteHref('docs/extending.md')).toBe('https://github.com/desktopmachineshop/OpenV/blob/master/docs/extending.md');
    expect(noteHref('https://openv.app/faq')).toBe('https://openv.app/faq');
    expect(noteHref('/manual/vv')).toBe('/manual/vv');
    expect(noteHref('#v0-21-4')).toBe('#v0-21-4');
  });
});

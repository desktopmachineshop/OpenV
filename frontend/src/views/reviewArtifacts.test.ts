import { ageLabel } from './reviewArtifacts';

// The age a review card prints in its corner.
describe('ageLabel', () => {
  const now = Date.parse('2026-11-03T12:00:00Z');

  it('counts minutes, hours and days', () => {
    expect(ageLabel('2026-11-03T11:59:40Z', now)).toBe('now');
    expect(ageLabel('2026-11-03T11:55:00Z', now)).toBe('5m');
    expect(ageLabel('2026-11-03T09:00:00Z', now)).toBe('3h');
    expect(ageLabel('2026-11-01T12:00:00Z', now)).toBe('2d');
  });

  it('says nothing for a missing or unreadable time', () => {
    expect(ageLabel(undefined, now)).toBe('');
    expect(ageLabel('not a date', now)).toBe('');
  });

  it('never prints a negative age for a clock a little ahead', () => {
    expect(ageLabel('2026-11-03T12:05:00Z', now)).toBe('now');
  });
});

import { formatLimit, isAlarming, limitSummary, usedFraction } from './OrgLimitsTab';
import { limitRefusal } from '../../api/errors';
import { LimitUsage } from '../../api/client';

const limit = (over: Partial<LimitUsage> = {}): LimitUsage => ({
  key: 'max_members',
  label: 'Workspace members',
  description: 'How many people can be in this workspace.',
  unit: 'count',
  limit: 10,
  unlimited: false,
  ...over,
});

// A number without its unit is a trap: 2048 members and 2048 MB look the same.
describe('rendering a limit', () => {
  it('reads storage as storage, and rounds up to GB where that is kinder', () => {
    expect(formatLimit(512, 'mb')).toBe('512 MB');
    expect(formatLimit(2048, 'mb')).toBe('2 GB');
    expect(formatLimit(1536, 'mb')).toBe('1.5 GB');
  });

  it('reads a whole number of hours as hours', () => {
    expect(formatLimit(45, 'minutes')).toBe('45 minutes');
    expect(formatLimit(60, 'minutes')).toBe('1 hour');
    expect(formatLimit(120, 'minutes')).toBe('2 hours');
    expect(formatLimit(90, 'minutes')).toBe('90 minutes');
  });

  it('does not pluralise a single CPU', () => {
    expect(formatLimit(1, 'cpus')).toBe('1 CPU');
    expect(formatLimit(2, 'cpus')).toBe('2 CPUs');
  });

  it('leaves a plain count alone', () => {
    expect(formatLimit(25, 'count')).toBe('25');
  });
});

describe('the one-line summary', () => {
  it('shows usage against the ceiling', () => {
    expect(limitSummary(limit({ used: 3 }))).toBe('3 of 10');
  });

  it('says there is no limit rather than showing a meaningless zero', () => {
    expect(limitSummary(limit({ unlimited: true, limit: 0 }))).toBe('No limit');
    expect(limitSummary(limit({ unlimited: true, limit: 0, used: 7 }))).toBe('7 used — no limit');
  });

  it('shows a ceiling alone when usage cannot be counted', () => {
    expect(limitSummary(limit({ unit: 'minutes', limit: 45, used: undefined }))).toBe('45 minutes');
  });
});

describe('the usage bar', () => {
  it('is not drawn when there is nothing to measure against', () => {
    expect(usedFraction(limit({ unlimited: true, limit: 0, used: 5 }))).toBeNull();
    expect(usedFraction(limit({ used: undefined }))).toBeNull();
  });

  it('measures usage against the ceiling', () => {
    expect(usedFraction(limit({ used: 5, limit: 10 }))).toBe(0.5);
  });

  // A workspace can be over its limit after a downgrade, and the bar must not
  // overflow its track.
  it('clamps a workspace that is already over', () => {
    expect(usedFraction(limit({ used: 30, limit: 10 }))).toBe(1);
  });
});

// A personal workspace seats one person and always will, so it is full from
// the moment it exists. Painting that red would teach people to ignore the
// colour on the limits where being full is actually a problem.
describe('a ceiling nothing raises', () => {
  const personalSeat = limit({ limit: 1, used: 1, fixed: true });

  it('is not treated as a warning even though it is full', () => {
    expect(usedFraction(personalSeat)).toBe(1);
    expect(isAlarming(personalSeat)).toBe(false);
  });

  it('still warns on a limit somebody could do something about', () => {
    expect(isAlarming(limit({ used: 10, limit: 10 }))).toBe(true);
    expect(isAlarming(limit({ used: 8, limit: 10 }))).toBe(true);
    expect(isAlarming(limit({ used: 3, limit: 10 }))).toBe(false);
    expect(isAlarming(limit({ unlimited: true, limit: 0, used: 900 }))).toBe(false);
  });

  it('reads as usage against its ceiling like any other', () => {
    expect(limitSummary(personalSeat)).toBe('1 of 1');
  });
});

// The plain message already carries the whole story; this reader is for the
// places that want to act on the numbers.
describe('reading a limit refusal', () => {
  const refusal = (data: unknown, status = 403) => ({ response: { status, data } });

  it('reads the numbers and the remedy', () => {
    const got = limitRefusal(
      refusal({
        code: 'limit_reached',
        limit: 'max_members',
        label: 'Workspace members',
        used: 5,
        allowed: 5,
        remedy: 'Upgrade the workspace’s plan to raise this limit.',
      })
    );
    expect(got).toEqual({
      limit: 'max_members',
      label: 'Workspace members',
      used: 5,
      allowed: 5,
      remedy: 'Upgrade the workspace’s plan to raise this limit.',
    });
  });

  it('ignores anything that is not a limit refusal', () => {
    expect(limitRefusal(refusal({ code: 'email_unverified' }))).toBeNull();
    expect(limitRefusal(refusal({ code: 'limit_reached' }, 500))).toBeNull();
    expect(limitRefusal(new Error('network'))).toBeNull();
    expect(limitRefusal(null)).toBeNull();
  });
});

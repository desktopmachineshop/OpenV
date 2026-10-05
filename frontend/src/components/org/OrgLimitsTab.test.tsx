import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { OrgLimitsTab, formatLimit, isAlarming, limitSummary, usedFraction } from './OrgLimitsTab';
import { limitRefusal } from '../../api/errors';
import { LimitUsage, orgsAPI } from '../../api/client';

vi.mock('../../api/client', async (orig) => mockApi(await orig()));

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

// A self-hosted deployment has no plans, so a feature its operator turned off
// is not "off the plan" there (#379 bug 199, the twin of the server's bug 192).
describe('a feature flag', () => {
  const teams = (included: boolean) =>
    limit({ key: 'teams', label: 'Teams and per-project access', unit: '', limit: 0, kind: 'flag', included });

  it('on a hosted workspace, reads as off the plan when the plan leaves it out', () => {
    expect(limitSummary(teams(false))).toBe('Not on this plan');
    expect(limitSummary(teams(false), false)).toBe('Not on this plan');
    expect(limitSummary(teams(true), false)).toBe('Included');
  });

  it('on a self-hosted deployment, reads as turned off on the deployment', () => {
    expect(limitSummary(teams(false), true)).toBe('Turned off on this deployment');
    expect(limitSummary(teams(false), true)).not.toMatch(/plan/i);
    expect(limitSummary(teams(true), true)).toBe('Included');
  });

  // The tab, not only the reader: it must hand the response's self_hosted on.
  describe('on the Limits tab', () => {
    (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

    let container: HTMLDivElement;
    let root: Root;

    beforeEach(() => {
      vi.resetAllMocks();
      container = document.createElement('div');
      document.body.appendChild(container);
      act(() => {
        root = createRoot(container);
      });
    });

    afterEach(() => {
      act(() => root.unmount());
      container.remove();
    });

    const renderWith = async (selfHosted: boolean) => {
      vi.mocked(orgsAPI.limits).mockResolvedValue({
        data: {
          org_id: 'org-1',
          plan: 'free',
          entitled_plan: 'free',
          plan_status: 'none',
          grandfathered: false,
          self_hosted: selfHosted,
          limits: [teams(false)],
        },
      } as any);
      await act(async () => {
        root.render(<OrgLimitsTab org={{ id: 'org-1', name: 'Acme' } as any} />);
      });
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0));
      });
      return container.textContent || '';
    };

    it('says a hosted workspace’s plan leaves the feature out', async () => {
      const text = await renderWith(false);
      expect(text).toContain('Not on this plan');
      expect(text).not.toContain('Turned off on this deployment');
    });

    it('says a self-hosted deployment has the feature turned off, naming no plan', async () => {
      const text = await renderWith(true);
      expect(text).toContain('Turned off on this deployment');
      expect(text).not.toContain('Not on this plan');
    });
  });
});

// A workspace past its ceilings is read-only on either kind of deployment,
// but only a hosted one has a plan to change: a self-hosted one is told which
// setting raises them, as the server's own refusal tells it (#379 bug 200).
describe('the read-only banner', () => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.resetAllMocks();
    container = document.createElement('div');
    document.body.appendChild(container);
    act(() => {
      root = createRoot(container);
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const bannerOf = async (selfHosted: boolean) => {
    vi.mocked(orgsAPI.limits).mockResolvedValue({
      data: {
        org_id: 'org-1',
        plan: 'free',
        entitled_plan: 'free',
        plan_status: 'none',
        grandfathered: false,
        self_hosted: selfHosted,
        read_only: true,
        over_plan: ['max_members'],
        limits: [limit({ used: 12, limit: 10 })],
      },
    } as any);
    await act(async () => {
      root.render(<OrgLimitsTab org={{ id: 'org-1', name: 'Acme' } as any} />);
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    const banner = Array.from(container.querySelectorAll('[role="alert"]')).find((el) =>
      el.textContent?.includes('This workspace is read-only.')
    );
    expect(banner).toBeDefined();
    return banner as HTMLElement;
  };

  it('tells a hosted workspace it holds more than its plan allows, and points to the Billing tab', async () => {
    const banner = await bannerOf(false);
    const text = banner.textContent || '';
    expect(text).toContain('It holds more than its plan allows (Workspace members).');
    expect(text).toContain('or on a plan that fits.');
    expect(text).toContain('A workspace admin can subscribe on the Billing tab, or remove members or delete projects.');
    expect(banner.querySelector('a')?.getAttribute('href')).toBe('/org/settings?tab=billing');
    expect(text).not.toContain('OPENV_LIMITS');
  });

  it('tells a self-hosted workspace which setting raises its limits, naming no plan', async () => {
    const banner = await bannerOf(true);
    const text = banner.textContent || '';
    expect(text).toContain('It holds more than this deployment’s limits allow (Workspace members).');
    expect(text).toContain('an administrator raises them in OPENV_LIMITS.');
    expect(text).not.toMatch(/plan/i);
    expect(text).not.toContain('Billing');
    expect(banner.querySelector('a')).toBeNull();
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

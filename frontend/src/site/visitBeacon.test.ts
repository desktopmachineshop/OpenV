import { beforeEach, describe, expect, it, vi } from 'vitest';
import { mockApi } from '../test/mockApi';

vi.mock('../api/client', async (orig) => mockApi(await orig(), { siteAPI: { visit: () => Promise.resolve({}) } }));

import { siteAPI } from '../api/client';
import { resetVisitBeacon, sendVisitBeacon } from './visitBeacon';

describe('sendVisitBeacon', () => {
  beforeEach(() => {
    resetVisitBeacon();
    vi.mocked(siteAPI.visit).mockClear();
    window.history.replaceState(null, '', '/pricing?utm_source=newsletter&plan=business');
  });

  it('sends once per signed-in state: the visit, then the sign-up that links it', () => {
    sendVisitBeacon(null);
    sendVisitBeacon(null);
    sendVisitBeacon('u1');
    sendVisitBeacon('u1');
    expect(siteAPI.visit).toHaveBeenCalledTimes(2);
    expect(siteAPI.visit).toHaveBeenNthCalledWith(1, { path: '/pricing', referrer: '', utm_source: 'newsletter' });
  });

  it("sends the referring site's origin only, and nothing for this site", () => {
    const referrer = vi.spyOn(document, 'referrer', 'get');
    referrer.mockReturnValue('https://news.ycombinator.com/item?id=42');
    sendVisitBeacon(null);
    expect(vi.mocked(siteAPI.visit).mock.calls[0][0].referrer).toBe('https://news.ycombinator.com');

    resetVisitBeacon();
    referrer.mockReturnValue(`${window.location.origin}/faq`);
    sendVisitBeacon(null);
    expect(vi.mocked(siteAPI.visit).mock.calls[1][0].referrer).toBe('');
    referrer.mockRestore();
  });

  it('never throws when the beacon fails', async () => {
    vi.mocked(siteAPI.visit).mockRejectedValueOnce(new Error('offline'));
    expect(() => sendVisitBeacon(null)).not.toThrow();
    await Promise.resolve();
  });
});

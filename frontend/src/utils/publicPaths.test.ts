import { registerSite, resetRegistry } from '../ext/registry';
import { isPublicPath } from './publicPaths';

const nothing = () => null;

afterEach(() => resetRegistry());

describe('isPublicPath', () => {
  it('accepts the front page, manual, login, verify-email and interview pages', () => {
    for (const p of ['/', '/manual', '/manual/getting-started', '/login', '/login?mode=register', '/interview/abc', '/verify-email', '/verify-email?token=abc']) {
      expect(isPublicPath(p)).toBe(true);
    }
  });

  it('rejects every signed-in route', () => {
    for (const p of ['/projects', '/projects/1/requirements', '/org/settings', '/manualx'.replace('x', '-not'), '/interviews']) {
      expect(isPublicPath(p)).toBe(false);
    }
  });

  it('accepts the pages of a registered site, and only once one is registered', () => {
    expect(isPublicPath('/pricing')).toBe(false);
    registerSite({ publicPaths: ['/pricing', '/compare'], Home: nothing, Pages: nothing });
    for (const p of ['/pricing', '/compare', '/compare/doors']) expect(isPublicPath(p)).toBe(true);
    expect(isPublicPath('/pricing-not')).toBe(false);
  });
});

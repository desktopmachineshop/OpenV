import { isPublicPath } from './publicPaths';

describe('isPublicPath', () => {
  it('accepts the landing, pricing, manual, login, verify-email and interview pages', () => {
    for (const p of ['/', '/pricing', '/manual', '/manual/getting-started', '/login', '/login?mode=register', '/interview/abc', '/verify-email', '/verify-email?token=abc']) {
      expect(isPublicPath(p)).toBe(true);
    }
  });

  it('rejects every signed-in route', () => {
    for (const p of ['/projects', '/projects/1/requirements', '/org/settings', '/manualx'.replace('x', '-not'), '/interviews']) {
      expect(isPublicPath(p)).toBe(false);
    }
  });
});

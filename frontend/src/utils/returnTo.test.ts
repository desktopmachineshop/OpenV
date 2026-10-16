import { forgetReturnTo, pendingReturnTo, rememberReturnTo, safeReturnPath } from './returnTo';

// Only an address inside the app is ever a place a sign-in goes back to,
// so a crafted ?next= cannot send a fresh session to another site.
describe('returnTo', () => {
  it('takes a path inside the app and nothing else', () => {
    expect(safeReturnPath('/connect?client_id=c1')).toBe('/connect?client_id=c1');
    for (const bad of [null, undefined, '', 'https://evil.example', '//evil.example/x', '/\\evil.example', 'connect']) {
      expect(safeReturnPath(bad)).toBe('');
    }
  });

  it('keeps a path across a single sign-on round trip until it is reached', () => {
    rememberReturnTo('//evil.example');
    expect(pendingReturnTo()).toBe('');
    rememberReturnTo('/connect?state=s');
    expect(pendingReturnTo()).toBe('/connect?state=s');
    forgetReturnTo();
    expect(pendingReturnTo()).toBe('');
  });
});

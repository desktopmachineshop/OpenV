import { pickActiveOrg } from './activeOrg';

// Where a sign-in lands. The server's answer carries the member's chosen
// default (or the switch made in this sign-in), so it outranks the browser's
// memory of where it last was; only this tab's own workspace comes first.
const orgs = [
  { id: 'personal', type: 'personal' },
  { id: 'acme', type: 'company' },
  { id: 'bigco', type: 'company' },
] as any[];

describe('picking the workspace to open in', () => {
  it('lands where the server says on a fresh sign-in, over the browser memory', () => {
    expect(pickActiveOrg(orgs, '', 'acme', 'personal')).toBe('acme');
  });

  it('keeps the tab where it was across a reload', () => {
    expect(pickActiveOrg(orgs, 'bigco', 'acme', 'personal')).toBe('bigco');
  });

  it('falls back to the browser memory, then the personal workspace', () => {
    expect(pickActiveOrg(orgs, '', '', 'bigco')).toBe('bigco');
    expect(pickActiveOrg(orgs, '', '', '')).toBe('personal');
  });

  // A remembered workspace the member has since left must not be chosen.
  it('ignores any workspace the member no longer belongs to', () => {
    expect(pickActiveOrg(orgs, 'gone', 'gone-too', 'gone-as-well')).toBe('personal');
    expect(pickActiveOrg([{ id: 'only', type: 'company' }] as any[], '', '', '')).toBe('only');
    expect(pickActiveOrg([], 'x', 'y', 'z')).toBe('');
  });
});

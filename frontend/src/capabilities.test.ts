import { describe, expect, it } from 'vitest';
import { CAPABILITY_BILLING, CAPABILITY_HOSTED, hasCapability } from './capabilities';

describe('hasCapability', () => {
  it('answers yes before the server has said, so nothing flickers away', () => {
    expect(hasCapability(null, CAPABILITY_HOSTED)).toBe(true);
    expect(hasCapability(null, CAPABILITY_BILLING)).toBe(true);
  });

  it('answers what the server listed once it has', () => {
    expect(hasCapability([CAPABILITY_HOSTED], CAPABILITY_HOSTED)).toBe(true);
    expect(hasCapability([CAPABILITY_HOSTED], CAPABILITY_BILLING)).toBe(false);
    // A self-hosted OpenV lists nothing: no public site, no plans.
    expect(hasCapability([], CAPABILITY_HOSTED)).toBe(false);
  });
});

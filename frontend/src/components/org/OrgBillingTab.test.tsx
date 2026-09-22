import { describe, expect, it } from 'vitest';
import { BillingState, PublicPlan } from '../../api/client';
import { pricePreview, statusLine, yearlySaving } from './OrgBillingTab';

const business: PublicPlan = {
  plan: 'business',
  per_seat: true,
  intervals: {
    month: { amounts: { gbp: 1500, usd: 2000 }, tax_behavior: 'exclusive' },
    year: { amounts: { gbp: 15000 }, tax_behavior: 'exclusive' },
  },
};

const lite: PublicPlan = {
  plan: 'business_lite',
  per_seat: false,
  intervals: { month: { amounts: { gbp: 900 } } },
};

const state = (billing: Partial<BillingState['billing']>, plan = 'business'): BillingState => ({
  org_id: 'o1',
  plan,
  entitled_plan: plan,
  granted: false,
  self_hosted: false,
  billing: { status: 'none', ...billing },
  plans: { billing_enabled: true, currencies: ['gbp'], plans: [business, lite] },
});

describe('pricePreview', () => {
  it('multiplies a per-seat price by the seats and says the tax', () => {
    expect(pricePreview(business, 'month', 'gbp', 7)).toBe('7 seats × £15 = £105 per month, plus VAT where it applies');
  });

  it('bills a flat plan once whatever the seat count', () => {
    expect(pricePreview(lite, 'month', 'gbp', 7)).toBe('£9 per month');
  });

  it('is null where the plan has no such price', () => {
    expect(pricePreview(lite, 'year', 'gbp', 1)).toBeNull();
    expect(pricePreview(business, 'year', 'usd', 1)).toBeNull();
  });

  it('never previews fewer than one seat', () => {
    expect(pricePreview(business, 'month', 'gbp', 0)).toContain('1 seat ×');
  });
});

describe('yearlySaving', () => {
  it('is the whole percentage a year saves against twelve months', () => {
    expect(yearlySaving(business, 'gbp')).toBe(17);
  });
  it('is null when a year is not cheaper or not offered', () => {
    expect(yearlySaving(lite, 'gbp')).toBeNull();
    expect(yearlySaving(business, 'usd')).toBeNull();
  });
});

describe('statusLine', () => {
  it('reads the free tier as nothing billed', () => {
    const line = statusLine(state({ status: 'none' }, 'single'));
    expect(line.title).toBe('Single User');
    expect(line.tone).toBe('muted');
  });

  it('names the trial end', () => {
    const line = statusLine(state({ status: 'trialing', period_end: '2026-10-06T00:00:00Z' }));
    expect(line.title).toBe('Business — trial');
    expect(line.body).toContain('2026');
    expect(line.tone).toBe('ok');
  });

  it('warns on a cancelled-at-period-end subscription', () => {
    const line = statusLine(state({ status: 'active', cancel_at_period_end: true, period_end: '2026-10-06T00:00:00Z' }));
    expect(line.body).toMatch(/^Cancelled: the plan stays until/);
    expect(line.tone).toBe('warn');
  });

  it('keeps the workspace working on a failed payment', () => {
    const line = statusLine(state({ status: 'past_due' }));
    expect(line.body).toBe('A payment failed. Your workspace keeps working — update the card to avoid interruption.');
    expect(line.tone).toBe('danger');
  });

  it.each(['canceled', 'unpaid', 'disputed', 'paused', 'incomplete'])('says everything stays exportable after %s', (status) => {
    const line = statusLine(state({ status }));
    expect(line.title).toContain('Single User');
    expect(line.body).toContain('still exportable');
  });
});

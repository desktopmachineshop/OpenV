import React, { act } from 'react';
import { readFileSync } from 'fs';
import { join } from 'path';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../../test/mockApi';
import { OrgBillingTab } from './OrgBillingTab';
import { billingAPI } from '../../api/client';
import { useAppStore } from '../../state/store';

// The Billing tab's buttons are drawn with classes the app's stylesheets
// define (#379, bug 121): they named btn and btn-primary, which none does,
// so they drew as bare browser buttons, as bug 107's did.

vi.mock('../../api/client', async (orig) =>
  mockApi(await orig(), {
    orgsAPI: { limits: () => Promise.resolve({ data: { limits: [], read_only: false } }) },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const plans = {
  billing_enabled: true,
  currencies: ['gbp'],
  plans: [
    { plan: 'business_lite', per_seat: false, intervals: { month: { amounts: { gbp: 900 } } } },
    { plan: 'business', per_seat: true, intervals: { month: { amounts: { gbp: 1500 } } } },
  ],
};
const billing = (status: string, plan = 'business_lite') => ({
  org_id: 'o1',
  plan,
  entitled_plan: plan,
  granted: false,
  self_hosted: false,
  billing: { status, interval: 'month' },
  plans,
});

// Both stylesheets index.tsx loads eagerly define the app's buttons.
const css = ['../../index.css', '../ProjectList.css'].map((f) => readFileSync(join(__dirname, f), 'utf8')).join('\n');
const defined = (cls: string) => new RegExp(`\\.${cls}(?![\\w-])`).test(css);

const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const flush = async () => {
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
};

const button = (text: string) => {
  const found = Array.from(container.querySelectorAll('button')).find((b) => (b.textContent ?? '').trim() === text);
  expect(found, `button "${text}"`).toBeTruthy();
  return found!;
};

const expectStyled = (b: HTMLButtonElement, className: string) => {
  expect(b.classList.length, b.textContent ?? '').toBeGreaterThan(0);
  for (const cls of Array.from(b.classList)) expect(defined(cls), `.${cls} on "${b.textContent}"`).toBe(true);
  expect(b.className, b.textContent ?? '').toBe(className);
};

const mount = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/org/settings?tab=billing']}>
        <OrgBillingTab org={{ id: 'o1', name: 'Acme' } as any} isAdmin />
      </MemoryRouter>
    );
  });
  await flush();
};

beforeEach(() => {
  vi.clearAllMocks();
  useAppStore.setState(
    { ...initialStore, features: { channel: 'nightly', stable_release: '', preview: false, features: { 'workspace-billing': true } } },
    true
  );
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
});

describe('OrgBillingTab buttons', () => {
  it('draws the checkout button as the app draws a primary button', async () => {
    vi.mocked(billingAPI.state).mockResolvedValue({ data: billing('none', 'single') } as any);
    await mount();
    expectStyled(button('Continue to Stripe'), 'button');
  });

  it('draws the subscription and plan change buttons as the app draws its buttons', async () => {
    vi.mocked(billingAPI.state).mockResolvedValue({ data: billing('active') } as any);
    await mount();
    expectStyled(button('Manage billing'), 'button-secondary');
    await act(async () => {
      button('Change plan').click();
    });
    await flush();
    // The picker's submit is the second "Change plan" now; the opener is gone.
    expectStyled(button('Change plan'), 'button');
    expectStyled(button('Keep the current plan'), 'button-secondary');
  });

  it('draws the card update after a failed payment as a primary button', async () => {
    vi.mocked(billingAPI.state).mockResolvedValue({ data: billing('past_due') } as any);
    await mount();
    expectStyled(button('Update the card'), 'button');
    expectStyled(button('Change plan'), 'button-secondary');
  });
});

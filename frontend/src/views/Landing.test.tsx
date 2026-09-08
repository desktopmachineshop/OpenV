import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { Landing } from './Landing';
import {
  ALPHA_NOTE,
  BUSINESS_LIFE_LIMITS,
  DATA_PROMISE,
  HOSTED_LIMITS,
  HOSTED_TIERS,
  OTHER_TIERS,
} from '../landing/content';

// CRA's Jest cannot resolve react-router v7's package exports, so the router
// is mocked with the two pieces the view uses: Link renders a plain anchor
// and the hooks return inert values.
jest.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) => require('react').createElement('a', { href: String(to), ...rest }, children),
  useNavigate: () => jest.fn(),
  useSearchParams: () => [new URLSearchParams((globalThis as any).__testSearch || ''), jest.fn()],
}));

// The landing page is static copy; what matters is that the hosting terms,
// the tiers, the limits and the data promise from landing/content.ts all
// reach the DOM, and that the calls to action point where they say.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

const render = async (el: React.ReactElement) => {
  await act(async () => {
    root.render(el);
  });
};

describe('Landing', () => {
  it('states every tier, the alpha note, both limit lists and the data promise', async () => {
    await render(<Landing />);
    const text = container.textContent || '';
    for (const tier of [...HOSTED_TIERS, ...OTHER_TIERS]) {
      expect(text).toContain(tier.name);
      expect(text).toContain(tier.summary);
      for (const point of tier.points) expect(text).toContain(point);
    }
    expect(text).toContain(ALPHA_NOTE);
    for (const line of [...HOSTED_LIMITS, ...BUSINESS_LIFE_LIMITS]) {
      expect(text).toContain(line);
    }
    expect(text).toContain(DATA_PROMISE);
    expect(text).toContain('ReqIF');
    expect(text).toContain('AGPL-3.0');
  });

  it('marks exactly the three paid tiers as coming soon, with one free sign-up in the hosted row', async () => {
    await render(<Landing />);
    const pricing = container.querySelector('#pricing') as HTMLElement;
    const chips = Array.from(pricing.querySelectorAll('span')).filter((el) => el.textContent === 'Coming soon');
    expect(chips).toHaveLength(3);
    const cards = Array.from(pricing.querySelectorAll('article'));
    expect(cards).toHaveLength(HOSTED_TIERS.length + OTHER_TIERS.length);
    const signUps = cards.filter((card) =>
      Array.from(card.querySelectorAll('a')).some((a) => a.getAttribute('href') === '/login?mode=register')
    );
    expect(signUps).toHaveLength(1);
  });

  it('links sign-in and registration to the login screen', async () => {
    await render(<Landing />);
    const hrefs = Array.from(container.querySelectorAll('a')).map((a) => a.getAttribute('href'));
    expect(hrefs).toContain('/login');
    expect(hrefs).toContain('/login?mode=register');
    expect(hrefs).toContain('/manual');
    expect(hrefs.some((h) => h?.startsWith('https://github.com/desktopmachineshop/OpenV'))).toBe(true);
  });

  it('never offers anything to buy', async () => {
    await render(<Landing />);
    const text = (container.textContent || '').toLowerCase();
    expect(text).not.toContain('upgrade');
    expect(text).not.toContain('per seat');
    expect(text).not.toContain('per month');
    expect(text).not.toContain('billing coming soon');
  });
});

import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { Landing } from './Landing';
import { DATA_PROMISE, HOSTED_LIMITS, PRICING_TIERS } from '../landing/content';

// CRA's Jest cannot resolve react-router v7's package exports, so the router
// is mocked with the two pieces the view uses: Link renders a plain anchor
// and the hooks return inert values.
jest.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) => require('react').createElement('a', { href: String(to), ...rest }, children),
  useNavigate: () => jest.fn(),
  useSearchParams: () => [new URLSearchParams((globalThis as any).__testSearch || ''), jest.fn()],
}));

// The landing page is static copy; what matters is that the hosting terms,
// the limits and the data promise from landing/content.ts all reach the DOM,
// and that the calls to action point where they say.

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
  it('states the three pricing tiers, the limits and the data promise', async () => {
    await render(<Landing />);
    const text = container.textContent || '';
    for (const tier of PRICING_TIERS) {
      expect(text).toContain(tier.name);
      expect(text).toContain(tier.summary);
    }
    for (const line of HOSTED_LIMITS) {
      expect(text).toContain(line);
    }
    expect(text).toContain(DATA_PROMISE);
    expect(text).toContain('ReqIF');
    expect(text).toContain('AGPL-3.0');
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
    expect(text).not.toContain('billing coming soon');
  });
});

import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { VerifyEmail } from './VerifyEmail';
import { useAppStore } from '../state/store';

// Same recipe as Login.test.tsx: CRA's Jest cannot resolve react-router v7,
// so the router pieces the view uses are mocked; Navigate renders a marker
// so a redirect is visible in the DOM.
jest.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) => require('react').createElement('a', { href: String(to), ...rest }, children),
  Navigate: ({ to }: any) => require('react').createElement('div', { 'data-navigate': String(to) }),
  useNavigate: () => jest.fn(),
  useSearchParams: () => [new URLSearchParams((globalThis as any).__testSearch || ''), jest.fn()],
}));

const calls: string[] = [];
jest.mock('../api/client', () => ({
  authAPI: {
    me: () => new Promise(() => {}),
    verifyEmail: (token: string) => {
      calls.push('verify:' + token);
      return new Promise(() => {});
    },
    resendVerification: () => {
      calls.push('resend');
      return Promise.resolve({ data: { sent_to: 'pending@example.com' } });
    },
    changeVerificationEmail: (email: string) => {
      calls.push('change:' + email);
      return Promise.resolve({ data: { sent_to: email } });
    },
    logout: () => Promise.resolve(),
  },
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  calls.length = 0;
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
  useAppStore.setState({ currentUser: null, emailVerificationRequired: false });
});

const pending = {
  id: 'u1',
  email: 'pending@example.com',
  name: 'Pending',
  avatar_url: '',
  auth_provider: 'password',
  is_admin: false,
  email_verified: false,
  created_at: '2026-09-08T00:00:00Z',
};

const render = async (path: string) => {
  (globalThis as any).__testSearch = path.includes('?') ? path.slice(path.indexOf('?')) : '';
  await act(async () => {
    root.render(<VerifyEmail />);
  });
};

const click = async (label: string) => {
  const button = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === label);
  if (!button) throw new Error(`no button "${label}" in: ${container.textContent}`);
  await act(async () => {
    button.click();
  });
};

describe('VerifyEmail', () => {
  it('walls an unverified account with the address and the actions', async () => {
    useAppStore.setState({ currentUser: pending as any, emailVerificationRequired: true });
    await render('/verify-email');
    expect(container.textContent).toContain('Check your inbox');
    expect(container.textContent).toContain('pending@example.com');
    for (const label of ['Resend email', "I've clicked the link", 'Use a different address', 'Sign out']) {
      expect(Array.from(container.querySelectorAll('button')).some((b) => b.textContent === label)).toBe(true);
    }
  });

  it('resends the link and starts the cooldown', async () => {
    useAppStore.setState({ currentUser: pending as any, emailVerificationRequired: true });
    await render('/verify-email');
    await click('Resend email');
    expect(calls).toEqual(['resend']);
    expect(container.textContent).toContain('Sent to pending@example.com');
    expect(container.textContent).toMatch(/Resend email \(\d+s\)/);
  });

  it('sends the link to a corrected address', async () => {
    useAppStore.setState({ currentUser: pending as any, emailVerificationRequired: true });
    await render('/verify-email');
    await click('Use a different address');
    const input = container.querySelector('input[type="email"]') as HTMLInputElement;
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    await act(async () => {
      setter.call(input, 'fixed@example.com');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await click('Send link there');
    expect(calls).toEqual(['change:fixed@example.com']);
    expect(container.textContent).toContain('fixed@example.com');
  });

  it('sends a verified account on to the app', async () => {
    useAppStore.setState({ currentUser: { ...pending, email_verified: true } as any, emailVerificationRequired: true });
    await render('/verify-email');
    expect(container.querySelector('[data-navigate="/projects"]')).not.toBeNull();
  });

  it('sends a visitor with no session to sign in', async () => {
    await render('/verify-email');
    expect(container.querySelector('[data-navigate="/login"]')).not.toBeNull();
  });

  it('confirms a token from the emailed link', async () => {
    await render('/verify-email?token=abc123');
    expect(calls).toEqual(['verify:abc123']);
    expect(container.textContent).toContain('Verifying your email');
  });
});

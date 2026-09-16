import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ResetPassword } from './ResetPassword';

// Password reset landing (REQ-158). Same router recipe as VerifyEmail.test:
// Navigate renders a marker, useNavigate hands back one recording function.
const navigations: string[] = [];
vi.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) => require('react').createElement('a', { href: String(to), ...rest }, children),
  Navigate: ({ to }: any) => require('react').createElement('div', { 'data-navigate': String(to) }),
  useNavigate: () => (to: string) => navigations.push(to),
  useSearchParams: () => [new URLSearchParams((globalThis as any).__testSearch || ''), vi.fn()],
}));

const calls: any[][] = [];
let mockConfirmError: any = null;
vi.mock('../api/client', () => ({
  DEFAULT_MIN_PASSWORD_LENGTH: 8,
  authAPI: {
    policy: () => Promise.resolve({ data: { registration: 'open', min_password_length: 10 } }),
    confirmPasswordReset: (...args: any[]) => {
      calls.push(args);
      return mockConfirmError ? Promise.reject(mockConfirmError) : Promise.resolve({});
    },
  },
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  calls.length = 0;
  navigations.length = 0;
  mockConfirmError = null;
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const render = async (search: string) => {
  (globalThis as any).__testSearch = search;
  await act(async () => {
    root.render(<ResetPassword />);
  });
};

const type = async (index: number, value: string) => {
  const field = container.querySelectorAll('input[type="password"]')[index] as HTMLInputElement;
  const setValue = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
  await act(async () => {
    setValue.call(field, value);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
};

const submit = async () => {
  await act(async () => {
    container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
};

describe('ResetPassword', () => {
  it('sends a visitor with no token to ask for one', async () => {
    await render('');
    expect(container.querySelector('[data-navigate]')?.getAttribute('data-navigate')).toBe('/login?mode=forgot');
  });

  it('states the server password rule and refuses a short or mismatched pair locally', async () => {
    await render('?token=abc');
    expect((container.querySelector('input[type="password"]') as HTMLInputElement).placeholder).toContain('min 10 characters');
    await type(0, 'short');
    await type(1, 'short');
    await submit();
    expect(container.textContent).toContain('at least 10 characters');
    await type(0, 'long-enough-password');
    await type(1, 'long-enough-passwordX');
    await submit();
    expect(container.textContent).toContain('do not match');
    expect(calls).toEqual([]);
  });

  it('spends the link and sends the person to sign in', async () => {
    await render('?token=abc');
    await type(0, 'long-enough-password');
    await type(1, 'long-enough-password');
    await submit();
    expect(calls).toEqual([['abc', 'long-enough-password']]);
    expect(navigations).toEqual(['/login?reset=done']);
  });

  it('explains a dead link and offers a new one', async () => {
    mockConfirmError = { response: { status: 400, data: { error: 'password reset link is invalid or has expired', code: 'reset_invalid' } } };
    await render('?token=stale');
    await type(0, 'long-enough-password');
    await type(1, 'long-enough-password');
    await submit();
    expect(container.textContent).toContain('This link did not work');
    expect(container.querySelector('a[href="/login?mode=forgot"]')).not.toBeNull();
    expect(navigations).toEqual([]);
  });
});

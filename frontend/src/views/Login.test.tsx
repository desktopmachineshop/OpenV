import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { Login } from './Login';

// CRA's Jest cannot resolve react-router v7's package exports, so the router
// is mocked with the two pieces the view uses: Link renders a plain anchor
// and the hooks return inert values.
jest.mock('react-router-dom', () => ({
  Link: ({ to, children, ...rest }: any) => require('react').createElement('a', { href: String(to), ...rest }, children),
  useNavigate: () => jest.fn(),
  useSearchParams: () => [new URLSearchParams((globalThis as any).__testSearch || ''), jest.fn()],
}));

// The login module builds an axios client at import time, so the API is
// mocked wholesale. config() and invitation() answer from mutable fixtures
// so a test can put the view on a closed deployment or hand it an invite
// link; me() never resolves, which is what "signed out" looks like here.
const authFixtures: {
  config: any;
  invitation: any;
  calls: { login: any[][]; register: any[][]; acceptInvitation: any[][] };
} = {
  config: null,
  invitation: null,
  calls: { login: [], register: [], acceptInvitation: [] },
};

const record = (name: 'login' | 'register' | 'acceptInvitation', data: any) => (...args: any[]) => {
  authFixtures.calls[name].push(args);
  return Promise.resolve({ data });
};

jest.mock('../api/client', () => ({
  authAPI: {
    config: () =>
      authFixtures.config
        ? Promise.resolve({ data: authFixtures.config })
        : new Promise(() => {}),
    me: () => new Promise(() => {}),
    invitation: () =>
      authFixtures.invitation
        ? Promise.resolve({ data: authFixtures.invitation })
        : Promise.reject(new Error('invalid invitation')),
    acceptInvitation: (...args: any[]) => record('acceptInvitation', {})(...args),
    login: (...args: any[]) =>
      record('login', { id: 'u1', email: 'member@example.com', email_verified: true })(...args),
    register: (...args: any[]) =>
      record('register', { id: 'u2', email: 'invited@example.com', email_verified: true })(...args),
    oidcLoginUrl: () => '/oidc',
    googleLoginUrl: () => '/google',
  },
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  authFixtures.config = null;
  authFixtures.invitation = null;
  authFixtures.calls = { login: [], register: [], acceptInvitation: [] };
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

const render = async (path: string) => {
  (globalThis as any).__testSearch = path.includes('?') ? path.slice(path.indexOf('?')) : '';
  await act(async () => {
    root.render(<Login />);
  });
};

// submitForm fills the password (the address comes prefilled from the invite
// preview) and submits, the way a person would.
const submitForm = async (password: string) => {
  const field = container.querySelector('input[type="password"]') as HTMLInputElement;
  const setValue = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
  await act(async () => {
    setValue.call(field, password);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
  const form = container.querySelector('form') as HTMLFormElement;
  await act(async () => {
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
  });
};

describe('Login', () => {
  it('opens in sign-in mode by default', async () => {
    await render('/login');
    expect(container.querySelector('input[placeholder="Your name"]')).toBeNull();
    expect(container.textContent).toContain('Sign in to your workspace');
  });

  it('opens registration when the landing page asks for it', async () => {
    await render('/login?mode=register');
    expect(container.querySelector('input[placeholder="Your name"]')).not.toBeNull();
    expect(container.textContent).toContain('Create your account');
  });

  it('links back to the landing page', async () => {
    await render('/login');
    const back = Array.from(container.querySelectorAll('a')).find((a) => a.getAttribute('href') === '/');
    expect(back).toBeDefined();
  });

  it('offers sign-up while registration is open', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open' };
    await render('/login');
    expect(container.textContent).toContain('Create a new account');
    expect(container.textContent).not.toContain('Registration is closed');
  });

  it('hides sign-up and says so when registration is closed', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    await render('/login');
    expect(container.textContent).not.toContain('Create a new account');
    expect(container.textContent).toContain(
      'Registration is closed; ask a workspace admin for an invitation.'
    );
  });

  // ?mode=register cannot conjure a sign-up form on a closed deployment.
  it('falls back to sign-in when registration is closed', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    await render('/login?mode=register');
    expect(container.querySelector('input[placeholder="Your name"]')).toBeNull();
    expect(container.textContent).toContain('Sign in to your workspace');
  });

  it('opens registration prefilled from an invite link', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    authFixtures.invitation = {
      email: 'invited@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    await render('/login?invite=tok-123');

    // The sign-up form is available even though registration is closed —
    // the invitation is the door — and the address comes prefilled.
    expect(container.querySelector('input[placeholder="Your name"]')).not.toBeNull();
    const email = container.querySelector('input[type="email"]') as HTMLInputElement;
    expect(email.value).toBe('invited@example.com');
    expect(container.textContent).toContain('Desktop Machine Shop');
    expect(container.textContent).not.toContain('Registration is closed');
  });

  // The token, not the address, is what grants the invited membership: it
  // has to reach the server with the sign-up.
  it('sends the invite token with a sign-up', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    authFixtures.invitation = {
      email: 'invited@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    await render('/login?invite=tok-123');
    await submitForm('secret-password');

    expect(authFixtures.calls.register).toHaveLength(1);
    expect(authFixtures.calls.register[0][3]).toBe('tok-123');
    // Sign-up carries the token itself; there is no second call.
    expect(authFixtures.calls.acceptInvitation).toHaveLength(0);
  });

  // Somebody who already has an account follows the same link, switches to
  // sign-in, and must still end up in the workspace.
  it('accepts the invite token after signing in with an existing account', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    authFixtures.invitation = {
      email: 'member@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    await render('/login?invite=tok-456');
    // "I already have an account — sign in".
    const toggle = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent || '').includes('I already have an account')
    ) as HTMLButtonElement;
    await act(async () => {
      toggle.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await submitForm('secret-password');

    expect(authFixtures.calls.login).toHaveLength(1);
    expect(authFixtures.calls.acceptInvitation).toEqual([['tok-456']]);
  });

  it('says so when the invite link no longer works', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    await render('/login?invite=expired');
    expect(container.textContent).toContain('This invitation link is invalid or has expired.');
    expect(container.textContent).toContain('Registration is closed');
  });
});

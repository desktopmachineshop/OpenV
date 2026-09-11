import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { Login } from './Login';

// CRA's Jest cannot resolve react-router v7's package exports, so the router
// is mocked with the two pieces the view uses: Link renders a plain anchor
// and the hooks return inert values. useNavigate hands back the SAME
// function every render, as the real one does — a fresh identity would make
// every effect that depends on it re-run on every render, which is not how
// the view behaves in the app.
jest.mock('react-router-dom', () => {
  const navigate = jest.fn();
  return {
    Link: ({ to, children, ...rest }: any) =>
      require('react').createElement('a', { href: String(to), ...rest }, children),
    useNavigate: () => navigate,
    useSearchParams: () => [new URLSearchParams((globalThis as any).__testSearch || ''), jest.fn()],
  };
});

// The login module builds an axios client at import time, so the API is
// mocked wholesale. config(), invitation() and me() answer from mutable
// fixtures so a test can put the view on a closed deployment, hand it an
// invite link, or sign a particular account in. A null `me` fixture never
// resolves, which is what "signed out" looks like here.
type Calls = {
  login: any[][];
  register: any[][];
  acceptInvitation: any[][];
  logout: any[][];
};

const authFixtures: {
  config: any;
  invitation: any;
  // null: the preview never resolves, which is what an in-flight (or very
  // slow) preview looks like to somebody who signs in straight away.
  invitationPending: boolean;
  policy: any;
  me: any;
  // What register() reports the invite token did, and whether accepting one
  // fails — the two ways a conversion can not happen.
  registerOutcome: string | null;
  acceptError: any;
  calls: Calls;
} = {
  config: null,
  invitation: null,
  invitationPending: false,
  policy: { registration: 'open', min_password_length: 8 },
  me: null,
  registerOutcome: null,
  acceptError: null,
  calls: { login: [], register: [], acceptInvitation: [], logout: [] },
};

const record = (name: keyof Calls, data: any) => (...args: any[]) => {
  authFixtures.calls[name].push(args);
  return Promise.resolve({ data });
};

jest.mock('../api/client', () => ({
  DEFAULT_MIN_PASSWORD_LENGTH: 8,
  authAPI: {
    policy: () => Promise.resolve({ data: authFixtures.policy }),
    config: () =>
      authFixtures.config
        ? Promise.resolve({ data: authFixtures.config })
        : new Promise(() => {}),
    me: () =>
      authFixtures.me ? Promise.resolve({ data: authFixtures.me }) : new Promise(() => {}),
    invitation: () =>
      authFixtures.invitationPending
        ? new Promise(() => {})
        : authFixtures.invitation
          ? Promise.resolve({ data: authFixtures.invitation })
          : Promise.reject(new Error('invalid invitation')),
    acceptInvitation: (...args: any[]) => {
      authFixtures.calls.acceptInvitation.push(args);
      return authFixtures.acceptError
        ? Promise.reject(authFixtures.acceptError)
        : Promise.resolve({ data: {} });
    },
    logout: (...args: any[]) => record('logout', {})(...args),
    login: (...args: any[]) =>
      record('login', { id: 'u1', email: 'member@example.com', email_verified: true })(...args),
    register: (...args: any[]) =>
      record('register', {
        id: 'u2',
        email: 'invited@example.com',
        email_verified: true,
        ...(authFixtures.registerOutcome ? { invitation: authFixtures.registerOutcome } : {}),
      })(...args),
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
  authFixtures.invitationPending = false;
  authFixtures.policy = { registration: 'open', min_password_length: 8 };
  authFixtures.me = null;
  authFixtures.registerOutcome = null;
  authFixtures.acceptError = null;
  authFixtures.calls = { login: [], register: [], acceptInvitation: [], logout: [] };
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

  // Signing in as one address must not take up an invitation sent to
  // another: the server refuses it (403), and the client does not ask.
  it('does not post the token when the signed-in address is not the invited one', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    authFixtures.invitation = {
      email: 'someone-else@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    await render('/login?invite=tok-789');
    const toggle = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent || '').includes('I already have an account')
    ) as HTMLButtonElement;
    await act(async () => {
      toggle.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    // login() answers as member@example.com, not the invited address.
    await submitForm('secret-password');

    expect(authFixtures.calls.login).toHaveLength(1);
    expect(authFixtures.calls.acceptInvitation).toHaveLength(0);
  });

  // A link opened in a browser somebody is already signed in on must not
  // quietly put that account into the workspace: the invitation is shown and
  // the person presses Join.
  it('shows a signed-in account the invitation instead of accepting it', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    authFixtures.me = { id: 'u1', email: 'member@example.com', email_verified: true };
    authFixtures.invitation = {
      email: 'member@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    await render('/login?invite=tok-abc');

    expect(authFixtures.calls.acceptInvitation).toHaveLength(0);
    // No credentials form: this browser has a session.
    expect(container.querySelector('input[type="password"]')).toBeNull();
    const join = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent || '').includes('Join Desktop Machine Shop')
    ) as HTMLButtonElement;
    expect(join).toBeDefined();

    await act(async () => {
      join.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(authFixtures.calls.acceptInvitation).toEqual([['tok-abc']]);
  });

  // The session belongs to somebody else: name the address the invitation is
  // for, offer to sign out, and call nothing.
  it('tells a signed-in account when the invitation is for another address', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    authFixtures.me = { id: 'u1', email: 'colleague@example.com', email_verified: true };
    authFixtures.invitation = {
      email: 'invited@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'admin',
      expires_at: '2030-01-01T00:00:00Z',
    };
    await render('/login?invite=tok-def');

    expect(container.textContent).toContain('This invitation is for');
    expect(container.textContent).toContain('invited@example.com');
    expect(container.textContent).toContain('Sign out and sign in with that address');
    expect(authFixtures.calls.acceptInvitation).toHaveLength(0);
    expect(
      Array.from(container.querySelectorAll('button')).find((b) =>
        (b.textContent || '').includes('Join ')
      )
    ).toBeUndefined();

    // Signing out puts the sign-in form back, on the same link.
    const signOut = Array.from(container.querySelectorAll('button')).find(
      (b) => (b.textContent || '').trim() === 'Sign out'
    ) as HTMLButtonElement;
    await act(async () => {
      signOut.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(authFixtures.calls.logout).toHaveLength(1);
    // The handler awaits the logout call before it clears the session, so
    // the state it sets lands a microtask later than the click itself.
    await act(async () => {});
    expect(container.querySelector('input[type="password"]')).not.toBeNull();
  });

  // The token converts only for the address it was issued to, so the field
  // is not the person's to change: an editable one only produces a sign-up
  // that silently joins nothing.
  it('locks the address to the invited one while an invite is loaded', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    authFixtures.invitation = {
      email: 'invited@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    await render('/login?invite=tok-123');

    const email = container.querySelector('input[type="email"]') as HTMLInputElement;
    expect(email.value).toBe('invited@example.com');
    expect(email.readOnly).toBe(true);
    // And the way out is named: drop the link.
    expect(container.textContent).toContain('This invitation was sent to invited@example.com');
  });

  // A sign-up that joined nothing must not pass in silence.
  it('says which address the link was issued to when the server reports a mismatch', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open' };
    authFixtures.invitation = {
      email: 'invited@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    authFixtures.registerOutcome = 'email_mismatch';
    await render('/login?invite=tok-123');
    await submitForm('secret-password');

    expect(authFixtures.calls.register).toHaveLength(1);
    expect(container.textContent).toContain(
      'This link was issued to invited@example.com; register with that address to join.'
    );
  });

  it('says so when the link was revoked before the sign-up landed', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open' };
    authFixtures.invitation = {
      email: 'invited@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    authFixtures.registerOutcome = 'invalid';
    await render('/login?invite=tok-123');
    await submitForm('secret-password');

    expect(container.textContent).toContain('no longer valid');
    expect(container.textContent).toContain('did not join the workspace');
    // The account exists and is signed in, so there is a way on from here.
    expect(
      Array.from(container.querySelectorAll('button')).find((b) =>
        (b.textContent || '').includes('Continue to OpenV')
      )
    ).toBeDefined();
  });

  // An accepted invitation is silent, as before: the person lands in the app.
  it('says nothing extra when the sign-up accepted the invitation', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open' };
    authFixtures.invitation = {
      email: 'invited@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    authFixtures.registerOutcome = 'accepted';
    await render('/login?invite=tok-123');
    await submitForm('secret-password');

    expect(container.textContent).not.toContain('did not join');
    expect(container.textContent).not.toContain('was issued to');
  });

  // A failed accept after signing in is reported, not swallowed: they asked
  // to join a workspace, and silence would leave them believing they did.
  it('surfaces an error when the invitation could not be accepted after sign-in', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    authFixtures.invitation = {
      email: 'member@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    authFixtures.acceptError = {
      response: { data: { error: 'invitation link is invalid or has expired' } },
    };
    await render('/login?invite=tok-456');
    const toggle = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent || '').includes('I already have an account')
    ) as HTMLButtonElement;
    await act(async () => {
      toggle.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await submitForm('secret-password');

    expect(authFixtures.calls.acceptInvitation).toEqual([['tok-456']]);
    expect(container.textContent).toContain('invitation link is invalid or has expired');
  });

  // The banner has to tell the person what to do with the form they are
  // looking at, not with the one the link opened.
  it('tells the invitee what to do in whichever mode is showing', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    authFixtures.invitation = {
      email: 'invited@example.com',
      org_name: 'Desktop Machine Shop',
      role: 'member',
      expires_at: '2030-01-01T00:00:00Z',
    };
    await render('/login?invite=tok-123');
    expect(container.textContent).toContain(
      'Create your account with invited@example.com to join.'
    );

    const toggle = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent || '').includes('I already have an account')
    ) as HTMLButtonElement;
    await act(async () => {
      toggle.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    expect(container.textContent).toContain('Sign in as invited@example.com to join.');
    expect(container.textContent).not.toContain('Create your account with');
  });

  it('says so when the invite link no longer works', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'closed' };
    await render('/login?invite=expired');
    expect(container.textContent).toContain('This invitation link is invalid or has expired.');
    expect(container.textContent).toContain('Registration is closed');
  });

  // Somebody with an account can sign in before the preview has come back.
  // The token must still be posted: dropping it would leave them signed in
  // and quietly not in the workspace they followed a link to join. Whether
  // the address matches is the server's call — it answers 403 — and it is
  // the only thing that can decide it here, because the preview that would
  // have said so has not arrived.
  it('posts the invite token when signing in before the preview resolves', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open' };
    authFixtures.invitationPending = true;
    await render('/login?invite=tok-early');
    const toggle = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent || '').includes('I already have an account')
    ) as HTMLButtonElement;
    await act(async () => {
      toggle.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await submitForm('secret-password');

    expect(authFixtures.calls.login).toHaveLength(1);
    expect(authFixtures.calls.acceptInvitation).toEqual([['tok-early']]);
  });

  // And when the server refuses that accept, the person is told rather than
  // being dropped into the app believing they joined.
  it('reports a refused accept when the preview never resolved', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open' };
    authFixtures.invitationPending = true;
    // The server's own words: this link was issued to another address.
    authFixtures.acceptError = {
      response: {
        status: 403,
        data: { error: 'this invitation was sent to a different address' },
      },
    };
    await render('/login?invite=tok-early');
    const toggle = Array.from(container.querySelectorAll('button')).find((b) =>
      (b.textContent || '').includes('I already have an account')
    ) as HTMLButtonElement;
    await act(async () => {
      toggle.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });
    await submitForm('secret-password');

    expect(authFixtures.calls.acceptInvitation).toEqual([['tok-early']]);
    expect(container.textContent).toContain('this invitation was sent to a different address');
    expect(
      Array.from(container.querySelectorAll('button')).find((b) =>
        (b.textContent || '').includes('Continue to OpenV')
      )
    ).toBeDefined();
  });

  // The sign-up form states the server's own password rule, not a copy of it.
  it('takes the minimum password length from the server policy', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open' };
    authFixtures.policy = { registration: 'open', min_password_length: 12 };
    await render('/login?mode=register');
    const field = container.querySelector('input[type="password"]') as HTMLInputElement;
    expect(field.placeholder).toBe('Password (min 12 characters)');
  });
});

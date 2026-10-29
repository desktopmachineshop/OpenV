import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../test/mockApi';
import { Login } from './Login';

// The router is mocked with the two pieces the view uses: Link renders a plain anchor
// and the hooks return inert values. useNavigate hands back the SAME
// function every render, as the real one does — a fresh identity would make
// every effect that depends on it re-run on every render, which is not how
// the view behaves in the app.
vi.mock('react-router-dom', () => {
  const navigate = vi.fn();
  return {
    Link: ({ to, children, ...rest }: any) =>
      require('react').createElement('a', { href: String(to), ...rest }, children),
    useNavigate: () => navigate,
    useSearchParams: () => [new URLSearchParams((globalThis as any).__testSearch || ''), vi.fn()],
  };
});

// Every client method is stubbed and the view's auth calls are overridden
// below: config(), invitation() and me() answer from mutable
// fixtures so a test can put the view on a closed deployment, hand it an
// invite link, or sign a particular account in. A null `me` fixture never
// resolves, which is what "signed out" looks like here.
type Calls = {
  login: any[][];
  register: any[][];
  acceptInvitation: any[][];
  logout: any[][];
  requestPasswordReset: any[][];
  confirmIdentityLink: any[][];
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
  // What requestPasswordReset answers with when it rejects; null resolves.
  resetError: any;
  // The pending sign-on link identityLink() names; null rejects (gone).
  identityLink: any;
  calls: Calls;
} = {
  config: null,
  invitation: null,
  invitationPending: false,
  policy: { registration: 'open', min_password_length: 8 },
  me: null,
  registerOutcome: null,
  acceptError: null,
  resetError: null,
  identityLink: null,
  calls: {
    login: [],
    register: [],
    acceptInvitation: [],
    logout: [],
    requestPasswordReset: [],
    confirmIdentityLink: [],
  },
};

const record = (name: keyof Calls, data: any) => (...args: any[]) => {
  authFixtures.calls[name].push(args);
  return Promise.resolve({ data });
};

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    authAPI: {
      policy: () => Promise.resolve({ data: authFixtures.policy }),
      config: () =>
        authFixtures.config
          ? Promise.resolve({ data: authFixtures.config })
          : new Promise(() => {}),
      me: () =>
        authFixtures.me === 'signed-out'
          ? Promise.reject(new Error('not authenticated'))
          : authFixtures.me
            ? Promise.resolve({ data: authFixtures.me })
            : new Promise(() => {}),
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
      identityLink: () =>
        authFixtures.identityLink
          ? Promise.resolve({ data: authFixtures.identityLink })
          : Promise.reject(new Error('no link')),
      confirmIdentityLink: (...args: any[]) =>
        record('confirmIdentityLink', { id: 'u1', email: 'dave@example.com', email_verified: true })(
          ...args
        ),
      oidcLoginUrl: (signUp?: boolean) => (signUp ? '/oidc?signup=1' : '/oidc'),
      googleLoginUrl: () => '/google',
      requestPasswordReset: (...args: any[]) => {
        authFixtures.calls.requestPasswordReset.push(args);
        return authFixtures.resetError
          ? Promise.reject(authFixtures.resetError)
          : Promise.resolve({ data: { sent_to: String(args[0]).trim().toLowerCase() } });
      },
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open' };
  authFixtures.invitation = null;
  authFixtures.invitationPending = false;
  authFixtures.policy = { registration: 'open', min_password_length: 8 };
  authFixtures.me = null;
  authFixtures.registerOutcome = null;
  authFixtures.acceptError = null;
  authFixtures.resetError = null;
  authFixtures.identityLink = null;
  authFixtures.calls = {
    login: [],
    register: [],
    acceptInvitation: [],
    logout: [],
    requestPasswordReset: [],
    confirmIdentityLink: [],
  };
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

  // Password reset (REQ-158).
  const clickText = async (text: string) => {
    const button = Array.from(container.querySelectorAll('button')).find((b) => b.textContent?.trim() === text);
    if (!button) throw new Error(`no button "${text}" in: ${container.textContent}`);
    await act(async () => {
      button.click();
    });
  };
  const typeEmail = async (value: string) => {
    const field = container.querySelector('input[type="email"]') as HTMLInputElement;
    const setValue = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
    await act(async () => {
      setValue.call(field, value);
      field.dispatchEvent(new Event('input', { bubbles: true }));
    });
  };

  it('offers a forgotten-password link that asks for an emailed reset', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open', password_reset_email: true };
    await render('/login');
    await clickText('Forgot your password?');
    expect(container.textContent).toContain('Reset your password');
    expect(container.querySelector('input[type="password"]')).toBeNull();
    await typeEmail(' Owner@Example.com ');
    await act(async () => {
      container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(authFixtures.calls.requestPasswordReset).toEqual([['Owner@Example.com']]);
    expect(container.textContent).toContain('owner@example.com');
    expect(container.textContent).toContain('a reset link is on its way');
    await clickText('Back to sign in');
    expect(container.textContent).toContain('Sign in to your workspace');
  });

  it('points at the administrator when the server cannot send email', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: false, registration: 'open', password_reset_email: false };
    await render('/login?mode=forgot');
    expect(container.textContent).toContain('cannot send email');
    expect(container.textContent).toContain('platform admin');
    expect(container.querySelector('input[type="email"]')).toBeNull();
  });

  it('says so after a reset landed the person back here', async () => {
    await render('/login?reset=done');
    expect(container.textContent).toContain('Your password has been changed');
  });

  // REQ-44: the consent an AI app asks for sends a signed-out member here
  // with ?next=; they go back there once signed in, and only ever to a page
  // of the app.
  it('goes back to the page that sent the person to sign in', async () => {
    const { useNavigate } = await import('react-router-dom');
    const navigate = vi.mocked(useNavigate());
    navigate.mockClear();
    authFixtures.me = { id: 'u1', email: 'member@example.com', email_verified: true };
    await render('/login?next=%2Fconnect%3Fclient_id%3Dc1');
    expect(navigate).toHaveBeenCalledWith('/connect?client_id=c1');

    navigate.mockClear();
    await render('/login?next=%2F%2Fevil.example%2F');
    expect(navigate).toHaveBeenCalledWith('/projects');
  });

  // Until the server has said which ways in it offers, the page shows
  // none: the password form must not flash on its way to the provider.
  it('renders no form while the sign-in methods load', async () => {
    authFixtures.config = null;
    await render('/login');
    expect(container.querySelector('form')).toBeNull();
    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    expect(container.textContent).toBe('');
  });

  it('explains a sign-on that came back with an error, beside the form', async () => {
    authFixtures.config = { google_enabled: false, oidc_enabled: true, oidc_provider_name: 'Okta', registration: 'open' };
    await render('/login?error=sso_cancelled');
    expect(container.textContent).toContain('Signing in through Okta was cancelled.');
    expect(container.querySelector('[role="alert"] a[href="/oidc"]')?.textContent).toBe('Try again');
    expect(container.querySelector('input[type="password"]')).not.toBeNull();
  });

  // A Google sign-on met an account made with a password: the page names the
  // account, locks its address, and the password confirms the link.
  it('connects Google to a password account with its password', async () => {
    const { useNavigate } = await import('react-router-dom');
    const navigate = vi.mocked(useNavigate());
    navigate.mockClear();
    authFixtures.identityLink = { provider: 'google', email: 'dave@example.com' };
    await render('/login?link=google');
    expect(container.textContent).toContain('An OpenV account already uses dave@example.com');
    const address = container.querySelector('input[type="email"]') as HTMLInputElement;
    expect(address.value).toBe('dave@example.com');
    expect(address.readOnly).toBe(true);
    expect(container.textContent).toContain('Connect Google and sign in');

    await submitForm('password1');
    expect(authFixtures.calls.confirmIdentityLink).toEqual([['password1']]);
    expect(authFixtures.calls.login).toEqual([]);
    expect(navigate).toHaveBeenCalledWith('/projects');
  });

  it('falls back to an ordinary sign-in when the link has expired', async () => {
    await render('/login?link=google');
    expect(container.textContent).toContain('That sign-in has expired');
    expect(container.textContent).not.toContain('Connect Google');
    await submitForm('password1');
    expect(authFixtures.calls.confirmIdentityLink).toEqual([]);
    expect(authFixtures.calls.login.length).toBe(1);
  });

  describe('where single sign-on is the only way in', () => {
    const ssoOnly = {
      google_enabled: false,
      oidc_enabled: true,
      oidc_only: true,
      oidc_provider_name: 'DMSid',
      password_enabled: false,
      email_verification_required: false,
      registration: 'open',
    };
    let assign: ReturnType<typeof vi.fn>;
    const original = window.location;
    beforeEach(() => {
      sessionStorage.clear();
      authFixtures.config = ssoOnly;
      authFixtures.me = 'signed-out';
      assign = vi.fn();
      Object.defineProperty(window, 'location', { configurable: true, value: { ...original, assign } });
    });
    afterEach(() => {
      Object.defineProperty(window, 'location', { configurable: true, value: original });
    });

    it('goes straight to the provider, with no password form or other buttons', async () => {
      await render('/login?next=%2Fconnect%3Fclient_id%3Dc1');
      expect(assign).toHaveBeenCalledWith('/oidc');
      expect(sessionStorage.getItem('openv_return_to')).toBe('/connect?client_id=c1');
      expect(container.textContent).toContain('Taking you to DMSid');
      expect(container.querySelector('a[href="/oidc"]')).not.toBeNull();
      expect(container.querySelector('input[type="password"]')).toBeNull();
      expect(container.textContent).not.toContain('Sign in with');
      expect(container.textContent).not.toContain('Create a new account');
    });

    it('sends sign-up to the provider\'s sign-up page', async () => {
      await render('/login?mode=register');
      expect(assign).toHaveBeenCalledWith('/oidc?signup=1');
      expect(container.textContent).toContain('Create your account');
    });

    it('comes back to a shared project after signing in', async () => {
      await render('/login?share=tok');
      expect(assign).toHaveBeenCalledWith('/oidc');
      expect(sessionStorage.getItem('openv_return_to')).toBe('/s/tok');
    });

    it('waits for the session check, and leaves a signed-in visitor to go home', async () => {
      const { useNavigate } = await import('react-router-dom');
      const navigate = vi.mocked(useNavigate());
      navigate.mockClear();
      authFixtures.me = { id: 'u1', email: 'member@example.com', email_verified: true };
      await render('/login');
      expect(navigate).toHaveBeenCalledWith('/projects');
      expect(assign).not.toHaveBeenCalled();
    });

    it('does not send the browser round again, but offers to try again', async () => {
      await render('/login');
      expect(assign).toHaveBeenCalledTimes(1);
      act(() => root.unmount());
      act(() => {
        root = createRoot(container);
      });
      await render('/login');
      expect(assign).toHaveBeenCalledTimes(1);
      expect(container.textContent).toContain('Signing in through DMSid did not finish');
      const retry = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Try again')!;
      await act(async () => retry.click());
      expect(assign).toHaveBeenCalledTimes(2);
    });

    it('stays put after a sign-out or a sign-on error', async () => {
      await render('/login?signed_out=1');
      expect(container.textContent).toContain('You are signed out of OpenV');
      expect(container.textContent).toContain('Continue with DMSid');
      await render('/login?error=sso_failed');
      expect(container.textContent).toContain('did not finish');
      expect(container.textContent).toContain('Try again');
      expect(assign).not.toHaveBeenCalled();
    });

    it('names the invitation and lets the invitee continue to sign up', async () => {
      authFixtures.invitation = { email: 'invited@example.com', org_name: 'Acme', role: 'member' };
      await render('/login?invite=tok');
      expect(assign).not.toHaveBeenCalled();
      expect(container.textContent).toContain('You have been invited to Acme as member');
      expect(container.textContent).toContain('as invited@example.com to join');
      const go = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Continue with DMSid')!;
      await act(async () => go.click());
      expect(assign).toHaveBeenCalledWith('/oidc?signup=1');
    });

    it('still links the provider to a password account with its password', async () => {
      authFixtures.identityLink = { provider: 'oidc', email: 'dave@example.com' };
      await render('/login?link=oidc');
      expect(assign).not.toHaveBeenCalled();
      expect(container.textContent).toContain('Connect DMSid and sign in');
      expect(container.textContent).not.toContain('Forgot your password?');
      expect(container.textContent).not.toContain('Sign in with DMSid');
      await submitForm('password1');
      expect(authFixtures.calls.confirmIdentityLink).toEqual([['password1']]);
    });

    // Someone who forgot the password the link step asks for resets it by
    // email, from that step, for the address being linked.
    it('offers a reset of the OpenV password from the link step', async () => {
      authFixtures.identityLink = { provider: 'oidc', email: 'dave@example.com' };
      await render('/login?link=oidc');
      const forgot = Array.from(container.querySelectorAll('button')).find(
        (b) => b.textContent === 'Forgot your OpenV password?'
      )!;
      await act(async () => forgot.click());
      expect(assign).not.toHaveBeenCalled();
      expect(container.textContent).toContain('Reset your password');
      expect(container.textContent).toContain('connects DMSid to your OpenV account once');
      expect((container.querySelector('input[type="email"]') as HTMLInputElement).value).toBe('dave@example.com');
      const form = container.querySelector('form') as HTMLFormElement;
      await act(async () => {
        form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
      });
      expect(authFixtures.calls.requestPasswordReset).toEqual([['dave@example.com']]);
      expect(container.textContent).toContain('a reset link is on its way');
    });

    it('explains each sign-on error in words of its own', async () => {
      await render('/login?error=sso_cancelled');
      expect(container.textContent).toContain('Signing in through DMSid was cancelled.');
      await render('/login?error=sso_unverified');
      expect(container.textContent).toContain('DMSid has not confirmed your email address');
      expect(container.textContent).toContain('Try again');
      expect(assign).not.toHaveBeenCalled();
    });

    it('stays put after the password was reset, and says what comes next', async () => {
      await render('/login?reset=done');
      expect(assign).not.toHaveBeenCalled();
      expect(container.textContent).toContain('Your OpenV password is set. Continue with DMSid');
      expect(container.querySelector('input[type="password"]')).toBeNull();
    });

    it('offers to try again when the link has expired', async () => {
      await render('/login?link=oidc');
      expect(assign).not.toHaveBeenCalled();
      expect(container.textContent).toContain('That sign-in has expired.');
      expect(container.querySelector('input[type="password"]')).toBeNull();
      expect(container.textContent).toContain('Try again');
    });
  });
});

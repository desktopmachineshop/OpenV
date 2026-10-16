import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { ClaudeSignInCard } from './ClaudeSignInCard';
import { providerTokensAPI, providerLoginsAPI } from '../../api/client';
import { useAppStore } from '../../state/store';

vi.mock('../../api/client', async (orig) => mockApi(await orig()));


const tokens = vi.mocked(providerTokensAPI);
const logins = vi.mocked(providerLoginsAPI);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const inAYear = new Date(Date.now() + 360 * 86_400_000).toISOString();

beforeEach(() => {
  vi.clearAllMocks();
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

const flush = () => act(async () => {});

const setInput = (input: HTMLInputElement, value: string) => {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
};

const status = (over: Record<string, unknown>) =>
  ({ data: { provider: 'claude-code', available: true, connected: false, expired: false, ...over } }) as any;

const setFeature = (on: boolean) =>
  useAppStore.setState({
    features: { channel: 'nightly', stable_release: '', preview: false, features: { 'cloud-runner-token': on } },
  } as any);

afterEach(() => setFeature(false));

const render = async (loggedIn = false) => {
  act(() => root.render(<ClaudeSignInCard loggedIn={loggedIn} onChanged={() => {}} />));
  await flush();
};

const stateLine = () => container.querySelector('[data-testid="claude-sign-in-state"]')!.textContent || '';

const button = (label: string) =>
  Array.from(container.querySelectorAll('button')).filter((b) => b.textContent === label);

describe('the Claude Code sign-in indicator', () => {
  it('says a saved token is in force and when it expires', async () => {
    tokens.get.mockResolvedValue(status({ connected: true, last4: 'WXYZ', expires_at: inAYear }));
    await render(true);
    expect(stateLine()).toContain('saved token (…WXYZ), expires');
    expect(stateLine()).not.toContain('session only');
  });

  it('says a sign-in is for this session only when no token is saved', async () => {
    tokens.get.mockResolvedValue(status({}));
    await render(true);
    expect(stateLine()).toContain('for this session only');
  });

  it('says not signed in, and that an expired token lapsed', async () => {
    tokens.get.mockResolvedValue(status({ connected: true, expired: true, last4: 'ABCD', expires_at: '2025-01-01T00:00:00Z' }));
    await render(false);
    expect(stateLine()).toContain('Not signed in');
    expect(stateLine()).toContain('expired on');
  });
});

describe('the ways to sign in', () => {
  it('offers this session and, with the feature, a year on a saved token', async () => {
    setFeature(true);
    tokens.get.mockResolvedValue(status({}));
    logins.startToken.mockResolvedValue({
      data: { id: 'l1', provider: 'claude-code', target: 'user', purpose: 'token', status: 'pending', auth_url: '', detail: 'Waiting for your cloud runner to pick this up.', created_at: '', updated_at: '' },
    } as any);
    await render();
    expect(container.textContent).toContain('This session only');
    expect(container.textContent).toContain('Stay signed in for a year');
    const [, yearly] = button('Sign in');
    await act(async () => {
      yearly.click();
    });
    expect(logins.startToken).toHaveBeenCalled();
    expect(logins.start).not.toHaveBeenCalled();
  });

  it('offers no saved token before the workspace has the feature, but keeps the paste', async () => {
    tokens.get.mockResolvedValue(status({}));
    await render();
    expect(container.textContent).not.toContain('Stay signed in for a year');
    expect(container.querySelector('input[aria-label="Claude subscription token"]')).not.toBeNull();
  });

  it('saves a pasted token and shows only its last four characters', async () => {
    tokens.get.mockResolvedValue(status({}));
    tokens.save.mockResolvedValue(status({ connected: true, last4: 'WXYZ', expires_at: inAYear }));
    await render();
    const input = container.querySelector('input[aria-label="Claude subscription token"]') as HTMLInputElement;
    act(() => setInput(input, '  sk-ant-oat01-secret-WXYZ  '));
    await act(async () => {
      (container.querySelector('form') as HTMLFormElement).requestSubmit();
    });
    expect(tokens.save).toHaveBeenCalledWith('claude-code', 'sk-ant-oat01-secret-WXYZ');
    expect(stateLine()).toContain('…WXYZ');
    expect(container.textContent).not.toContain('secret');
  });

  it('on a server that keeps no tokens, offers the session sign-in only', async () => {
    setFeature(true);
    tokens.get.mockResolvedValue(status({ available: false }));
    await render();
    expect(container.textContent).toContain('This session only');
    expect(container.textContent).not.toContain('Stay signed in');
    expect(container.querySelector('input')).toBeNull();
    expect(container.textContent).toContain('OPENV_SECRETS_KEY');
  });
});

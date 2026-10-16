import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { SubscriptionTokenCard } from './SubscriptionTokenCard';
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

it('saves a pasted token and then shows only its last four characters', async () => {
  tokens.get.mockResolvedValue({ data: { provider: 'claude-code', available: true, connected: false, expired: false } } as any);
  tokens.save.mockResolvedValue({
    data: { provider: 'claude-code', available: true, connected: true, last4: 'WXYZ', expires_at: inAYear, expired: false },
  } as any);
  act(() => root.render(<SubscriptionTokenCard />));
  await flush();

  const input = container.querySelector('input[aria-label="Claude subscription token"]') as HTMLInputElement;
  expect(input.type).toBe('password');
  act(() => setInput(input, '  sk-ant-oat01-secret-WXYZ  '));
  await act(async () => {
    (container.querySelector('form') as HTMLFormElement).requestSubmit();
  });
  expect(tokens.save).toHaveBeenCalledWith('claude-code', 'sk-ant-oat01-secret-WXYZ');
  expect(container.textContent).toContain('…WXYZ');
  expect(container.textContent).not.toContain('secret');
  expect(container.querySelector('form')).toBeNull();
});

it('says when the saved token has expired', async () => {
  tokens.get.mockResolvedValue({
    data: { provider: 'claude-code', available: true, connected: true, last4: 'ABCD', expires_at: '2025-01-01T00:00:00Z', expired: true },
  } as any);
  act(() => root.render(<SubscriptionTokenCard />));
  await flush();
  expect(container.textContent).toContain('expired');
  expect(container.textContent).toContain('claude setup-token');
});

it('explains a server that keeps no tokens', async () => {
  tokens.get.mockResolvedValue({ data: { provider: 'claude-code', available: false, connected: false, expired: false } } as any);
  act(() => root.render(<SubscriptionTokenCard />));
  await flush();
  expect(container.textContent).toContain('OPENV_SECRETS_KEY');
  expect(container.querySelector('input')).toBeNull();
});

describe('getting the token from a cloud runner', () => {
  const setFeature = (on: boolean) =>
    useAppStore.setState({
      features: { channel: 'nightly', stable_release: '', preview: false, features: { 'cloud-runner-token': on } },
    } as any);

  afterEach(() => setFeature(false));

  it('offers it when no token is saved, and starts a token request on the runner', async () => {
    setFeature(true);
    tokens.get.mockResolvedValue({ data: { provider: 'claude-code', available: true, connected: false, expired: false } } as any);
    logins.startToken.mockResolvedValue({
      data: { id: 'l1', provider: 'claude-code', target: 'user', purpose: 'token', status: 'pending', auth_url: '', detail: 'Waiting for your cloud runner to pick this up.', created_at: '', updated_at: '' },
    } as any);
    act(() => root.render(<SubscriptionTokenCard />));
    await flush();
    const mint = container.querySelector('[data-testid="mint-token"]');
    expect(mint).not.toBeNull();
    const button = Array.from(mint!.querySelectorAll('button')).find((b) => b.textContent === 'Get token')!;
    await act(async () => {
      button.click();
    });
    expect(logins.startToken).toHaveBeenCalled();
    expect(logins.start).not.toHaveBeenCalled();
    expect(container.textContent).toContain('Waiting for your cloud runner');
  });

  it('is not offered before the workspace has the feature', async () => {
    tokens.get.mockResolvedValue({ data: { provider: 'claude-code', available: true, connected: false, expired: false } } as any);
    act(() => root.render(<SubscriptionTokenCard />));
    await flush();
    expect(container.querySelector('[data-testid="mint-token"]')).toBeNull();
  });
});

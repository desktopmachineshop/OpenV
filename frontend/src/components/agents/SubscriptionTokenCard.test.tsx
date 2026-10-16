import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { SubscriptionTokenCard } from './SubscriptionTokenCard';
import { providerTokensAPI } from '../../api/client';

vi.mock('../../api/client', async (orig) => mockApi(await orig()));

const tokens = vi.mocked(providerTokensAPI);

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

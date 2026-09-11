import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { ProviderConnectCard, pasteKind } from './ProviderConnectCard';

// The relayed sign-in has to work on a phone (REQ-108): the paste-back field
// takes a paste from a password manager, opens the right keyboard and submits
// with its send key, and the auth URL is a real link the phone's browser can
// follow. jsdom cannot measure a viewport, so the phone-specific *layout* is
// covered by e2e/tools/phone-audit.js; what is asserted here is the input
// contract, which is the same at every width.

// Prefixed with "mock" so Jest allows the module factory below to close
// over them.
const mockSubmitted: string[] = [];
const mockLogin = {
  status: 'awaiting_code',
  detail: 'Open the sign-in link, authorize, then paste the code you are given back here.',
};

jest.mock('../../api/client', () => ({
  providerLoginsAPI: {
    start: () =>
      Promise.resolve({
        data: {
          id: 'pl1',
          provider: 'claude-code',
          target: 'user',
          status: mockLogin.status,
          auth_url: 'https://claude.ai/oauth/authorize?client=openv',
          detail: mockLogin.detail,
          created_at: '',
          updated_at: '',
        },
      }),
    get: () => new Promise(() => {}),
    submitCode: (id: string, code: string) => {
      mockSubmitted.push(`${id}:${code}`);
      return Promise.resolve({ data: {} });
    },
    cancel: () => new Promise(() => {}),
  },
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const render = async () => {
  await act(async () => {
    root.render(
      <ProviderConnectCard provider="claude-code" loggedIn={false} target="user" onComplete={() => {}} />
    );
  });
};

const connect = async () => {
  const button = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Connect');
  await act(async () => {
    button!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
};

const field = () => container.querySelector('input') as HTMLInputElement;

beforeEach(() => {
  mockSubmitted.length = 0;
  mockLogin.status = 'awaiting_code';
  mockLogin.detail = 'Open the sign-in link, authorize, then paste the code you are given back here.';
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
});

test('the paste-back field is set up for a phone keyboard and a password manager', async () => {
  await render();
  await connect();

  const input = field();
  expect(input).toBeTruthy();
  expect(input.getAttribute('enterkeyhint')).toBe('send');
  expect(input.getAttribute('inputmode')).toBe('text');
  expect(input.getAttribute('autocapitalize')).toBe('none');
  expect(input.getAttribute('autocorrect')).toBe('off');
  expect(input.getAttribute('spellcheck')).toBe('false');
  expect(input.getAttribute('autocomplete')).toBe('off');
});

test('a loopback flow asks for an address and opens a URL keyboard', async () => {
  mockLogin.detail =
    'Open the sign-in link and authorize. Copy the whole address from your browser’s address bar and paste it here.';
  await render();
  await connect();

  expect(field().getAttribute('inputmode')).toBe('url');
  const submit = Array.from(container.querySelectorAll('button')).map((b) => b.textContent);
  expect(submit).toContain('Submit address');
});

test('the keyboard send key submits the pasted code', async () => {
  await render();
  await connect();

  const input = field();
  await act(async () => {
    // What a password manager does: set the value and fire input.
    const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
    setter.call(input, ' code-from-manager ');
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(async () => {
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  });

  expect(mockSubmitted).toEqual(['pl1:code-from-manager']);
});

test('the authorization link opens in the phone browser', async () => {
  await render();
  await connect();

  const link = container.querySelector('a') as HTMLAnchorElement;
  expect(link.getAttribute('href')).toBe('https://claude.ai/oauth/authorize?client=openv');
  expect(link.getAttribute('target')).toBe('_blank');
  expect(link.getAttribute('rel')).toContain('noopener');
});

test('the Paste button appears only where the clipboard can be read', async () => {
  const original = (navigator as any).clipboard;

  Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
  await render();
  await connect();
  expect(Array.from(container.querySelectorAll('button')).map((b) => b.textContent)).not.toContain('Paste');
  await act(async () => root.unmount());

  root = createRoot(container);
  Object.defineProperty(navigator, 'clipboard', {
    value: { readText: () => Promise.resolve('pasted-code') },
    configurable: true,
  });
  await render();
  await connect();
  const paste = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Paste');
  expect(paste).toBeTruthy();
  await act(async () => {
    paste!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  expect(field().value).toBe('pasted-code');

  Object.defineProperty(navigator, 'clipboard', { value: original, configurable: true });
});

test('pasteKind reads what the worker asked for', () => {
  expect(pasteKind(null)).toBe('text');
  expect(pasteKind({ detail: 'paste the code you are given back here' } as any)).toBe('text');
  expect(pasteKind({ detail: 'copy the whole address from the address bar' } as any)).toBe('url');
  expect(pasteKind({ detail: 'paste the URL here' } as any)).toBe('url');
});

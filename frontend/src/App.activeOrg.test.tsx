import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from './test/mockApi';
import App from './App';
import { useAppStore } from './state/store';

// The workspace the app opens in at boot, as App reads it: this tab's own
// workspace (sessionStorage) first, then the server's answer (the switch made
// in this sign-in, else the member's default workspace), then the browser's
// last used one (localStorage). App hands pickActiveOrg the two storages
// apart; merging them into one "remembered" value would let the browser's
// memory outrank the server (#379, test gap 99).

const ORGS = [
  { id: 'personal', name: 'Mine', type: 'personal' },
  { id: 'acme', name: 'Acme', type: 'company' },
  { id: 'bigco', name: 'Bigco', type: 'company' },
];
let serverActive = '';

const ok = (data: unknown) => Promise.resolve({ data });

vi.mock('./api/client', async (orig) =>
  mockApi(await orig(), {
    authAPI: {
      me: () => ok({ id: 'u1', email: 'dana@example.com', name: 'Dana', email_verified: true }),
      config: () => ok({ email_verification_required: false }),
    },
    orgsAPI: { list: () => ok({ orgs: ORGS, active_org: serverActive }) },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const KEY = 'openv_active_org';
const initialStore = useAppStore.getState();
let container: HTMLDivElement;
let root: Root;

const boot = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={['/faq']}>
        <App />
      </MemoryRouter>
    );
  });
  await act(async () => {
    for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  });
  return useAppStore.getState().activeOrgId;
};

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  serverActive = '';
  useAppStore.setState(initialStore, true);
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  useAppStore.setState(initialStore, true);
  sessionStorage.clear();
  localStorage.clear();
});

describe('the workspace App opens in', () => {
  it("lands where the server says on a new sign-in, over the browser's last used workspace", async () => {
    localStorage.setItem(KEY, 'personal');
    serverActive = 'acme';
    expect(await boot()).toBe('acme');
  });

  it("keeps this tab's own workspace over the server's answer", async () => {
    sessionStorage.setItem(KEY, 'bigco');
    localStorage.setItem(KEY, 'personal');
    serverActive = 'acme';
    expect(await boot()).toBe('bigco');
  });

  it("falls back to the browser's last used workspace when the server names none", async () => {
    localStorage.setItem(KEY, 'bigco');
    expect(await boot()).toBe('bigco');
  });
});

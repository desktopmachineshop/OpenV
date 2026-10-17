import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { mockApi } from '../test/mockApi';
import { OAuthConsent } from './OAuthConsent';
import { connectedAppsAPI } from '../api/client';
import { useAppStore } from '../state/store';

// The consent an AI app asks for (REQ-44): the page names the app and
// where the browser goes back to, offers the member's workspaces and what
// the app may do, never more than it asked for, and sends the browser to
// whatever address the API answers with. Where a workspace may give an app
// more than one workspace, they are a checklist instead of a choice of one.

const consentApp = vi.hoisted(() => ({ client_id: 'c1', client_name: 'Claude', redirect_host: 'claude.ai' }));

vi.mock('../api/client', async (orig) =>
  mockApi(await orig(), {
    connectedAppsAPI: {
      consent: () => Promise.resolve({ data: { ...consentApp } }),
      decide: vi.fn(() => Promise.resolve({ data: { redirect_to: 'https://claude.ai/cb?code=x&state=s' } })),
    },
    orgsAPI: {
      list: () =>
        Promise.resolve({
          data: {
            orgs: [
              { id: 'personal', name: "Sam's Space" },
              { id: 'acme', name: 'Acme Robotics' },
            ],
            active_org: 'acme',
          },
        }),
    },
  })
);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

const decide = vi.mocked(connectedAppsAPI.decide);
let container: HTMLDivElement;
let root: Root;
let assign: ReturnType<typeof vi.fn>;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  assign = vi.fn();
  Object.defineProperty(window, 'location', { value: { ...window.location, assign }, writable: true });
  sessionStorage.setItem('openv_return_to', '/connect?client_id=c1');
  consentApp.redirect_host = 'claude.ai';
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  decide.mockClear();
  useAppStore.setState({ features: null });
});

const withSeveralWorkspaces = () =>
  useAppStore.setState({
    features: {
      channel: 'nightly',
      stable_release: '',
      preview: false,
      features: { 'connected-app-workspaces': true },
    },
  });

const QUERY =
  'client_id=c1&redirect_uri=https%3A%2F%2Fclaude.ai%2Fcb&code_challenge=abc&code_challenge_method=S256&state=s';

const render = async (query: string) => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[`/connect?${query}`]}>
        <OAuthConsent />
      </MemoryRouter>
    );
  });
};

const button = (label: string) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;

describe('the consent page', () => {
  it('names the app, offers the workspaces with the active one chosen, and read and change', async () => {
    await render(QUERY);
    expect(container.querySelector('h1')?.textContent).toBe('Connect Claude to OpenV');
    expect(container.textContent).toContain('claude.ai');
    const select = container.querySelector('#consent-workspace') as HTMLSelectElement;
    expect(Array.from(select.options).map((o) => o.textContent)).toEqual(["Sam's Space", 'Acme Robotics']);
    expect(select.value).toBe('acme');
    expect(container.querySelectorAll('input[type="radio"]').length).toBe(2);
    // The sign-in that led here is done with.
    expect(sessionStorage.getItem('openv_return_to')).toBeNull();
  });

  it('approves with the chosen workspace and scope and goes where the API says', async () => {
    await render(QUERY);
    const radios = container.querySelectorAll('input[type="radio"]');
    await act(async () => (radios[1] as HTMLInputElement).click()); // read only
    await act(async () => button('Connect').click());
    expect(decide).toHaveBeenCalledWith({
      client_id: 'c1',
      redirect_uri: 'https://claude.ai/cb',
      code_challenge: 'abc',
      code_challenge_method: 'S256',
      state: 's',
      scope: 'openv:read',
      org_ids: ['acme'],
      approve: true,
    });
    expect(assign).toHaveBeenCalledWith('https://claude.ai/cb?code=x&state=s');
  });

  it('gives the app several workspaces where that is available', async () => {
    withSeveralWorkspaces();
    await render(QUERY);
    expect(container.querySelector('#consent-workspace')).toBeNull();
    const boxes = Array.from(container.querySelectorAll('input[type="checkbox"]')) as HTMLInputElement[];
    expect(boxes.map((b) => b.checked)).toEqual([false, true]);
    // The last one chosen cannot be unticked: an app reaches one at least.
    expect(boxes[1].disabled).toBe(true);
    await act(async () => boxes[0].click());
    await act(async () => button('Connect').click());
    expect(decide.mock.calls[0][0].org_ids).toEqual(['acme', 'personal']);
  });

  it('declines with Cancel', async () => {
    await render(QUERY);
    await act(async () => button('Cancel').click());
    expect(decide.mock.calls[0][0]).toMatchObject({ approve: false, scope: 'openv:read openv:write' });
  });

  it('offers nothing beyond read when the app asked only to read', async () => {
    await render(QUERY + '&scope=openv%3Aread');
    expect(container.querySelectorAll('input[type="radio"]').length).toBe(1);
    await act(async () => button('Connect').click());
    expect(decide.mock.calls[0][0].scope).toBe('openv:read');
  });

  it('warns when the app answers on this computer, and not otherwise', async () => {
    await render(QUERY);
    expect(container.querySelector('[role="note"]')).toBeNull();
    act(() => root.unmount());
    root = createRoot(container);
    consentApp.redirect_host = 'localhost:3118';
    await render(QUERY);
    expect(container.querySelector('[role="note"]')?.textContent).toContain('runs on your own computer');
  });

  it('says so when the link is incomplete', async () => {
    await render('client_id=c1');
    expect(container.querySelector('h1')?.textContent).toBe('This link is incomplete');
    expect(button('Connect')).toBeUndefined();
  });
});

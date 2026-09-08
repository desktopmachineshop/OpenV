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
// mocked wholesale; neither call it makes on mount needs to resolve for the
// mode to be decided.
jest.mock('../api/client', () => ({
  authAPI: {
    config: () => new Promise(() => {}),
    me: () => new Promise(() => {}),
    login: () => Promise.reject(new Error('not in this test')),
    register: () => Promise.reject(new Error('not in this test')),
    oidcLoginUrl: () => '/oidc',
    googleLoginUrl: () => '/google',
  },
}));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
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
});

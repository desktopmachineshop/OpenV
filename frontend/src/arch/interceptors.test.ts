// Behavioural guard, no snapshot (nothing to regenerate; the file snapshots
// beside it regenerate with: npx vitest run src/arch -u).
//
// Pins what the real axios instance in api/client does around every request
// (invariants I22 and I23, OpenV REQ-114):
//   - X-Org-ID comes from sessionStorage, then localStorage, key
//     openv_active_org, read at request time; no key, no header;
//   - a 401 sends the page to /login, unless the page is public or the call
//     was to /api/v1/auth/ (so signing in can fail without a bounce);
//   - a 403 with code email_unverified sends the page to /verify-email,
//     unless it is already there;
//   - the error still reaches the caller either way.
// The network is replaced by an axios adapter; window.location by a plain
// object, because jsdom does not implement navigation.
import { AxiosError, type AxiosAdapter, type InternalAxiosRequestConfig } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import client from '../api/client';

const ORG_KEY = 'openv_active_org';

type Reply = { status: number; data?: unknown };
let reply: Reply = { status: 200, data: {} };
let seen: InternalAxiosRequestConfig[] = [];
const originalAdapter = client.defaults.adapter;

const adapter: AxiosAdapter = async (config) => {
  seen.push(config);
  const response = { status: reply.status, statusText: '', data: reply.data, headers: {}, config };
  if (reply.status >= 400) {
    throw new AxiosError(`Request failed with status code ${reply.status}`, 'ERR_BAD_REQUEST', config, null, response);
  }
  return response;
};

let location: { pathname: string; href: string };
const at = (pathname: string) => {
  location = { pathname, href: `http://localhost:3000${pathname}` };
  vi.stubGlobal('location', location);
};

/** Make one GET and return what came back and where the page was sent. */
async function call(url: string, r: Reply) {
  reply = r;
  const before = location.href;
  let error: unknown = null;
  try {
    await client.get(url);
  } catch (e) {
    error = e;
  }
  return { error, navigatedTo: location.href === before ? null : location.href };
}

beforeEach(() => {
  client.defaults.adapter = adapter;
  seen = [];
  sessionStorage.clear();
  localStorage.clear();
  vi.spyOn(console, 'error').mockImplementation(() => {});
  at('/projects');
});

afterEach(() => {
  client.defaults.adapter = originalAdapter;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
  localStorage.clear();
});

describe('X-Org-ID request header', () => {
  const header = () => seen[seen.length - 1].headers['X-Org-ID'];

  it('prefers the tab (sessionStorage) over the last-used workspace (localStorage)', async () => {
    sessionStorage.setItem(ORG_KEY, 'org-tab');
    localStorage.setItem(ORG_KEY, 'org-last');
    await call('/api/v1/projects', { status: 200 });
    expect(header()).toBe('org-tab');
  });

  it('falls back to localStorage', async () => {
    localStorage.setItem(ORG_KEY, 'org-last');
    await call('/api/v1/projects', { status: 200 });
    expect(header()).toBe('org-last');
  });

  it('is read at request time, not at import', async () => {
    await call('/api/v1/projects', { status: 200 });
    sessionStorage.setItem(ORG_KEY, 'org-later');
    await call('/api/v1/projects', { status: 200 });
    expect(header()).toBe('org-later');
  });

  it('is absent when neither storage holds the key, or storage throws', async () => {
    await call('/api/v1/projects', { status: 200 });
    expect(header()).toBeUndefined();
    localStorage.setItem(ORG_KEY, 'org-last');
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage blocked');
    });
    const { error } = await call('/api/v1/projects', { status: 200 });
    expect(error).toBeNull();
    expect(header()).toBeUndefined();
  });

  it('is sent on public calls too (the interceptor does not look at the URL)', async () => {
    sessionStorage.setItem(ORG_KEY, 'org-tab');
    await call('/api/v1/public/share/abc', { status: 200 });
    expect(header()).toBe('org-tab');
  });
});

describe('401 redirect', () => {
  it('sends a protected page to /login and still rejects', async () => {
    const { error, navigatedTo } = await call('/api/v1/projects', { status: 401, data: { error: 'unauthorized' } });
    expect(navigatedTo).toBe('/login');
    expect((error as AxiosError).response?.status).toBe(401);
    expect(console.error).toHaveBeenCalledWith('API Error:', expect.objectContaining({ status: 401, url: '/api/v1/projects' }));
  });

  it.each(['/projects/p1/requirements', '/org/settings', '/admin', '/whats-new'])(
    'redirects from the signed-in page %s',
    async (page) => {
      at(page);
      expect((await call('/api/v1/projects', { status: 401 })).navigatedTo).toBe('/login');
    }
  );

  it.each([
    '/',
    '/login',
    '/pricing',
    '/manual',
    '/manual/getting-started',
    '/interview/tok',
    '/verify-email',
    '/reset-password',
    '/share/tok',
    '/s/tok',
    '/open-source',
    '/open-source/p1',
    '/how-it-works',
    '/demos',
    '/faq',
    '/customers',
    '/white-papers',
    '/security',
  ])('never moves a visitor off the public page %s', async (page) => {
    at(page);
    const { error, navigatedTo } = await call('/api/v1/projects', { status: 401 });
    expect(navigatedTo).toBeNull();
    expect((error as AxiosError).response?.status).toBe(401);
  });

  it('does not redirect when the failing call is an /api/v1/auth/ call', async () => {
    const { error, navigatedTo } = await call('/api/v1/auth/me', { status: 401 });
    expect(navigatedTo).toBeNull();
    expect(error).toBeInstanceOf(AxiosError);
  });

  it('ignores other failures', async () => {
    for (const status of [400, 403, 404, 500]) {
      expect((await call('/api/v1/projects', { status, data: { error: 'x' } })).navigatedTo).toBeNull();
    }
  });
});

describe('email_unverified redirect', () => {
  const unverified = { status: 403, data: { error: 'verify your email', code: 'email_unverified' } };

  it('sends the page to /verify-email and still rejects', async () => {
    const { error, navigatedTo } = await call('/api/v1/projects', unverified);
    expect(navigatedTo).toBe('/verify-email');
    expect((error as AxiosError).response?.status).toBe(403);
  });

  it('applies on public pages too', async () => {
    at('/pricing');
    expect((await call('/api/v1/projects', unverified)).navigatedTo).toBe('/verify-email');
  });

  it('does not redirect from /verify-email itself', async () => {
    at('/verify-email');
    expect((await call('/api/v1/auth/verify-email/resend', unverified)).navigatedTo).toBeNull();
  });

  it('needs the code: another 403 stays put', async () => {
    const { navigatedTo } = await call('/api/v1/projects', { status: 403, data: { error: 'no', code: 'forbidden' } });
    expect(navigatedTo).toBeNull();
  });
});

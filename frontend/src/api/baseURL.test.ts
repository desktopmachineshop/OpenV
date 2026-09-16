// The API base URL decides whether the browser's session cookie is
// first-party: production builds must default to the page's own origin
// (nginx proxies /api/ there) unless a separate API origin was configured.
//
// The values are read from import.meta.env, which Vite substitutes at build
// time, so each case stubs the environment and re-imports the module rather
// than reassigning process.env as it did before.
describe('getAPIBaseURL', () => {
  const load = async (overrides: Record<string, string | undefined>) => {
    vi.resetModules();
    vi.unstubAllEnvs();
    for (const [key, value] of Object.entries(overrides)) {
      vi.stubEnv(key, value as string);
    }
    const { getAPIBaseURL } = await import('./baseURL');
    return getAPIBaseURL();
  };

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  it('uses a configured API origin, without a trailing slash', async () => {
    expect(await load({ REACT_APP_API_URL: 'https://api.example.com/' })).toBe(
      'https://api.example.com'
    );
  });

  it('defaults production builds to the same origin', async () => {
    expect(await load({ MODE: 'production', REACT_APP_API_URL: '' })).toBe('');
  });

  it('falls back to port 8080 on the dev server host', async () => {
    expect(await load({ MODE: 'development', REACT_APP_API_URL: undefined })).toBe(
      'http://localhost:8080'
    );
  });
});

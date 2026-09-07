// The API base URL decides whether the browser's session cookie is
// first-party: production builds must default to the page's own origin
// (nginx proxies /api/ there) unless a separate API origin was configured.
describe('getAPIBaseURL', () => {
  const env = process.env;

  const load = (overrides: Record<string, string | undefined>) => {
    let result = '';
    jest.isolateModules(() => {
      process.env = { ...env, ...overrides };
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      result = require('./client').getAPIBaseURL();
    });
    return result;
  };

  afterEach(() => {
    process.env = env;
  });

  it('uses a configured API origin, without a trailing slash', () => {
    expect(load({ REACT_APP_API_URL: 'https://api.example.com/' })).toBe('https://api.example.com');
  });

  it('defaults production builds to the same origin', () => {
    expect(load({ NODE_ENV: 'production', REACT_APP_API_URL: '' })).toBe('');
  });

  it('falls back to port 8080 on the dev server host', () => {
    expect(load({ NODE_ENV: 'development', REACT_APP_API_URL: undefined })).toBe('http://localhost:8080');
  });
});

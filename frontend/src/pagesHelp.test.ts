import { PAGES, helpKeyForPath, pagePattern } from './pages';

// Every page a signed-in member reaches sends the manual a context key, so
// the help panel opens on that page's topic and never on the default. That
// each key has a topic is docs.py's check (`sync --check`), which reads the
// keys from here and from every useHelpContext, HelpLink and dialog.
describe('help keys', () => {
  it('every signed-in page has a key', () => {
    const missing = PAGES.filter((p) => !p.public && !p.help.key).map(pagePattern);
    expect(missing).toEqual([]);
  });

  it('keys are dotted, lower-case names', () => {
    for (const page of PAGES) {
      if (page.help.key) expect(page.help.key, pagePattern(page)).toMatch(/^[a-z][a-z0-9-]*(\.[a-z0-9-]+)*$/);
    }
  });

  it('finds the key for a URL', () => {
    expect(helpKeyForPath('/projects/p1')).toBe('overview');
    expect(helpKeyForPath('/projects/p1/requirements')).toBe('requirements');
    expect(helpKeyForPath('/projects/p1/matrix')).toBe('matrix');
    expect(helpKeyForPath('/projects/p1/vv/runs/r9')).toBe('vv.runs');
    expect(helpKeyForPath('/projects/p1/crew/network')).toBe('crew');
    expect(helpKeyForPath('/projects/p1/settings')).toBe('settings');
    expect(helpKeyForPath('/org/settings')).toBe('org.settings');
    expect(helpKeyForPath('/org/runs')).toBe('org.runs');
    expect(helpKeyForPath('/whats-new')).toBe('whats-new');
    expect(helpKeyForPath('/admin')).toBe('admin');
    expect(helpKeyForPath('/login')).toBe('');
    expect(helpKeyForPath('/no/such/page')).toBe('');
  });
});

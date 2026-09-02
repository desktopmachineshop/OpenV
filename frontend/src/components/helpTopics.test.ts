import { helpTopicForPath } from './helpTopics';

// The help bubble is only useful if it lands on the topic for the page the
// reader is actually looking at, so the route→topic mapping is worth pinning.
describe('helpTopicForPath', () => {
  it('gives the project list its own topic, not the in-project default', () => {
    const topic = helpTopicForPath('/projects');
    expect(topic.title).toBe('Projects');
    // The page is about getting a project into existence, so the tips have to
    // cover both routes in and the per-card actions.
    const tips = topic.tips.join(' ').toLowerCase();
    ['new project', 'import', 'template', 'export', 'workspace'].forEach((subject) => {
      expect(tips).toContain(subject);
    });
  });

  it('still resolves the overview and module topics inside a project', () => {
    // /projects/:id is the overview — the list topic must not swallow it.
    expect(helpTopicForPath('/projects/abc-123').title).toBe('Project overview');
    expect(helpTopicForPath('/projects/abc-123/requirements').title).toBe('Requirements');
    // Deeper routes key off their module segment.
    expect(helpTopicForPath('/projects/abc-123/baselines/b1/compare').title).toBe(
      'Baseline comparison'
    );
  });

  it('links only to manual chapters that exist', () => {
    // Slugs are deep-linked as /manual/<slug>; a typo would render a dead link.
    const knownSlugs = [
      'getting-started',
      'projects',
      'product-overview',
      'artifacts',
      'links',
      'guided-wizard',
      'board',
      'agents',
      'crews',
      'runs-runners',
      'automations',
      'interviews',
      'vv',
      'org-settings',
      'troubleshooting',
    ];
    const topic = helpTopicForPath('/projects');
    expect(knownSlugs).toContain(topic.chapter.slug);
    (topic.also ?? []).forEach((extra) => expect(knownSlugs).toContain(extra.slug));
  });

  it('falls back rather than throwing on an unknown path', () => {
    expect(helpTopicForPath('/').title).toBe('Project overview');
    expect(helpTopicForPath('/nonsense/deep/path').title).toBe('Project overview');
  });
});

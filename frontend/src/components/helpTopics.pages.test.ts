import { helpTopicForPath } from './helpTopics';

// The help topic of every page of a project, by the first segment after the
// project id (refactor plan X17's characterization). helpTopics.test.ts pins
// the mapping's shape; this pins which topic each page reaches, including the
// four pages with no topic of their own (evidence, review, todos, impact),
// which fall back to the overview's, and the routes the menu does not list.
// A page registry that names each page's topic must say what is here.
describe('the help topic of each project page', () => {
  it.each([
    ['', 'Project overview'],
    ['guided', 'Guided Definition'],
    ['requirements', 'Requirements'],
    ['interviews', 'Interviews'],
    ['vv', 'V&V'],
    ['evidence', 'Project overview'],
    ['matrix', 'Traceability Matrix'],
    ['impact', 'Project overview'],
    ['review', 'Project overview'],
    ['board', 'Board'],
    ['todos', 'Project overview'],
    ['agents', 'Agents'],
    ['crew', 'Crew'],
    ['automations', 'Automations'],
    ['agent-runs', 'Runs'],
    ['activity', 'Activity'],
    ['settings', 'Project Settings'],
    ['crew/network', 'Crew'],
    ['vv/runs/r1', 'V&V'],
    ['baselines/b1/compare', 'Baseline comparison'],
    ['team', 'Project overview'],
    ['team/network', 'Project overview'],
  ])('/projects/p1/%s shows %j', (segment, title) => {
    expect(helpTopicForPath(`/projects/p1/${segment}`).title).toBe(title);
  });

  it('shows the topic of the project list on /projects alone', () => {
    expect(helpTopicForPath('/projects').title).toBe('Projects');
  });

  it('keys outside a project off the first segment', () => {
    expect(helpTopicForPath('/vv').title).toBe('V&V');
    expect(helpTopicForPath('/settings').title).toBe('Project Settings');
  });
});

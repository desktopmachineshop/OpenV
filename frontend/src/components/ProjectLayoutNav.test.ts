import type { NavSection } from '../pages';
import { activeGroup, groupLanding, loadLastPages, refreshSections, rememberGroupPage } from './ProjectLayoutNav';

// The refreshed menu (ui-refresh): the groups in the order Plan, Define,
// Verify, Agents, with Overview at the head of Plan and neither Summary nor
// Design listed, and a group's tab returning to the page last read there.

const sidebar: NavSection[] = [
  {
    label: 'Define',
    items: [
      { to: 'home', label: 'Summary' },
      { to: '', label: 'Overview', end: true },
      { to: 'guided', label: 'Guided Definition' },
      { to: 'requirements', label: 'Requirements' },
    ],
  },
  { label: 'Verify', items: [{ to: 'vv', label: 'V&V' }, { to: 'review', label: 'Review Queue' }] },
  { label: 'Plan', items: [{ to: 'board', label: 'Board' }, { to: 'gantt', label: 'Gantt' }] },
  { label: 'Agents', items: [{ to: 'agents', label: 'Agents' }] },
  { items: [{ to: 'activity', label: 'Activity' }, { to: 'settings', label: 'Settings' }] },
];

const shape = (sections: NavSection[]) => sections.map((s) => `${s.label ?? '-'}: ${s.items.map((i) => i.label).join(', ')}`);

afterEach(() => localStorage.clear());

describe('refreshSections', () => {
  it('orders the groups Plan, Define, Verify, Agents, heads Plan with Overview and drops Summary', () => {
    expect(shape(refreshSections(sidebar))).toEqual([
      'Plan: Overview, Board, Gantt',
      'Define: Guided Definition, Requirements',
      'Verify: V&V, Review Queue',
      'Agents: Agents',
      '-: Activity, Settings',
    ]);
  });

  it('drops Design, whose items Requirements holds', () => {
    const withDesign = sidebar.map((s) => (s.label === 'Define' ? { ...s, items: [...s.items, { to: 'design', label: 'Design' }] } : s));
    expect(shape(refreshSections(withDesign))[1]).toBe('Define: Guided Definition, Requirements');
  });

  it('keeps Overview current only on its own path', () => {
    expect(refreshSections(sidebar)[0].items[0]).toEqual({ to: '', label: 'Overview', end: true });
  });

  it('drops a group left with no page', () => {
    const gated = sidebar.filter((s) => s.label !== 'Agents');
    expect(shape(refreshSections(gated))).toEqual([
      'Plan: Overview, Board, Gantt',
      'Define: Guided Definition, Requirements',
      'Verify: V&V, Review Queue',
      '-: Activity, Settings',
    ]);
  });
});

describe('the group in view and where its tab leads', () => {
  const menu = refreshSections(sidebar);

  it('names the group of the page in view, and none for a page outside every group', () => {
    expect(activeGroup(menu, '')?.label).toBe('Plan');
    expect(activeGroup(menu, 'requirements')?.label).toBe('Define');
    expect(activeGroup(menu, 'settings')).toBeUndefined();
  });

  it('leads to the first page of a group nobody has read yet', () => {
    expect(groupLanding(menu[1], loadLastPages())).toBe('guided');
    expect(groupLanding(menu[0], loadLastPages())).toBe('');
  });

  it('returns to the page last read in the group', () => {
    rememberGroupPage(menu, 'requirements');
    rememberGroupPage(menu, 'review');
    expect(loadLastPages()).toEqual({ Define: 'requirements', Verify: 'review' });
    expect(groupLanding(menu[1], loadLastPages())).toBe('requirements');
    expect(groupLanding(menu[2], loadLastPages())).toBe('review');
  });

  it('remembers nothing for a page outside every group', () => {
    rememberGroupPage(menu, 'settings');
    expect(loadLastPages()).toEqual({});
  });

  it('falls back to the first page when the remembered one has left the menu', () => {
    rememberGroupPage(menu, 'guided');
    const withoutGuided = menu.map((s) => ({ ...s, items: s.items.filter((i) => i.to !== 'guided') }));
    expect(groupLanding(withoutGuided[1], loadLastPages())).toBe('requirements');
  });

  it('reads broken storage as nothing remembered', () => {
    localStorage.setItem('openv-nav-last-page', '[1]');
    expect(loadLastPages()).toEqual({});
    localStorage.setItem('openv-nav-last-page', '{"Plan":3,"Define":"requirements"}');
    expect(loadLastPages()).toEqual({ Define: 'requirements' });
  });
});

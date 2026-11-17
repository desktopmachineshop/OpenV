import { assistantRoute, draftTestsMessage } from './floatingAssistantRoute';
import { isAssistantShortcut } from './floatingAssistantLayout';
import { openAssistant, useAssistantStore } from './assistantStore';

describe('assistantRoute (#726)', () => {
  it('shows on the workspace Home with no project', () => {
    expect(assistantRoute('/projects')).toEqual({ show: true, projectId: null, pageLabel: 'Home' });
  });

  it('shows on every project page, named as the menu names it', () => {
    expect(assistantRoute('/projects/p1')).toEqual({ show: true, projectId: 'p1', pageLabel: 'Overview' });
    expect(assistantRoute('/projects/p1/vv')).toMatchObject({ show: true, pageLabel: 'V&V' });
    expect(assistantRoute('/projects/p1/board/')).toMatchObject({ show: true, pageLabel: 'Board' });
    expect(assistantRoute('/projects/p1/vv/runs/r9')).toMatchObject({ pageLabel: 'Test run' });
    expect(assistantRoute('/projects/p1/baselines/b1/compare')).toMatchObject({ pageLabel: 'Baseline comparison' });
  });

  it('shows on the workspace pages too, with no project in the address', () => {
    expect(assistantRoute('/org/plan')).toEqual({ show: true, projectId: null, pageLabel: 'Workspace plan' });
    expect(assistantRoute('/org/plan/tasks')).toMatchObject({ show: true, pageLabel: 'My tasks' });
    expect(assistantRoute('/org/settings')).toMatchObject({ show: true, projectId: null, pageLabel: 'Workspace settings' });
    expect(assistantRoute('/admin')).toMatchObject({ show: true, pageLabel: 'Platform admin' });
  });

  it('stays off the guided wizard, which docks its own assistant, and off other pages', () => {
    expect(assistantRoute('/projects/p1/guided').show).toBe(false);
    expect(assistantRoute('/pricing').show).toBe(false);
    expect(assistantRoute('/login').show).toBe(false);
  });
});

describe('draftTestsMessage (#728)', () => {
  it('names every gap by ref, title, kind and id, and says what it was given', () => {
    const message = draftTestsMessage([
      { id: 'a1', ref: 'REQ-153', title: 'Brake light', why: 'no test' },
      { id: 'a2', ref: 'REQ-88', title: 'Backup and restore', why: 'unverified' },
    ]);
    expect(message.split('\n')[0]).toBe('Drafting test cases for 2 gaps: REQ-153, REQ-88.');
    expect(message).toContain('- REQ-153 Brake light (no test; artifact id a1)');
    expect(message).toContain('- REQ-88 Backup and restore (unverified; artifact id a2)');
    expect(message).toContain('verifies link');
  });

  it('says "1 gap" for one', () => {
    expect(draftTestsMessage([{ id: 'a1', title: 'Untitled', why: 'no test' }]).split('\n')[0]).toBe(
      'Drafting test cases for 1 gap: Untitled.'
    );
  });
});

describe('isAssistantShortcut', () => {
  it('is Alt+A by key position, and nothing with another modifier', () => {
    expect(isAssistantShortcut({ code: 'KeyA', key: 'å', altKey: true })).toBe(true);
    expect(isAssistantShortcut({ code: 'KeyA', key: 'a', altKey: true, ctrlKey: true })).toBe(false);
    expect(isAssistantShortcut({ code: 'KeyA', key: 'a' })).toBe(false);
    expect(isAssistantShortcut({ code: 'KeyA', key: 'a', altKey: true, defaultPrevented: true })).toBe(false);
  });
});

describe('openAssistant', () => {
  beforeEach(() => {
    useAssistantStore.setState({ panel: 'closed', page: {}, override: null, dismissed: null, request: null });
  });

  it('opens the panel and queues one turn with the context it was given', () => {
    openAssistant({ message: '  Draft these  ', context: { label: '2 V&V gaps' } });
    const s = useAssistantStore.getState();
    expect(s.panel).toBe('open');
    expect(s.request?.message).toBe('Draft these');
    expect(s.override).toEqual({ label: '2 V&V gaps' });
    s.takeRequest(s.request!.id);
    expect(useAssistantStore.getState().request).toBeNull();
  });

  it('keeps an expanded panel expanded, and opens with no turn when given no message', () => {
    useAssistantStore.setState({ panel: 'expanded' });
    openAssistant();
    expect(useAssistantStore.getState()).toMatchObject({ panel: 'expanded', request: null });
  });
});

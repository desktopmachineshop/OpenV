import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter, useNavigate } from 'react-router-dom';
import { HelpProvider, useHelp, useHelpContext } from './useHelp';
import { HelpLink } from '../components/ui/HelpLink';
import { useAppStore } from '../state/store';

// What the help panel shows: a help link's key, else the innermost active
// context (a dialog over a tab over the page), else the page's key, and
// that it follows each as they come and go. ? and F1 open it, and nothing
// of it shows without the manual-help gate.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let state: ReturnType<typeof useHelp>;
let go: (to: string) => void;
const Probe: React.FC = () => {
  state = useHelp();
  go = useNavigate();
  return null;
};
const Context: React.FC<{ k: string | null; dialog?: boolean }> = ({ k, dialog }) => {
  useHelpContext(k, { dialog });
  return null;
};

const gate = (on: boolean) =>
  useAppStore.setState({
    capabilities: on ? ['help-docs'] : [],
    activeOrgId: 'org-1',
    features: { channel: 'nightly', stable_release: '', preview: false, features: { 'manual-help': on } },
  });

describe('useHelp', () => {
  let container: HTMLDivElement;
  let root: Root;
  beforeEach(() => {
    gate(true);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    useAppStore.setState({ features: null, capabilities: null, activeOrgId: null } as any);
  });

  const render = (children: React.ReactNode, at = '/projects/p1/vv') =>
    act(() => {
      root.render(
        <MemoryRouter initialEntries={[at]}>
          <HelpProvider>
            <Probe />
            {children}
          </HelpProvider>
        </MemoryRouter>,
      );
    });

  it('shows the page key, and follows the route', () => {
    render(null);
    expect(state.current).toEqual({ key: 'vv' });
    act(() => go('/projects/p1/matrix'));
    expect(state.current.key).toBe('matrix');
  });

  it('a tab outranks the page, a dialog the tab, and closing returns', () => {
    render(<Context k="settings.members" />, '/projects/p1/settings');
    expect(state.current.key).toBe('settings.members');
    // A dialog mounted with the tab still wins, whatever the mount order.
    render(
      <>
        <Context k="settings.members" />
        <Context k="projects.download" dialog />
      </>,
      '/projects/p1/settings',
    );
    expect(state.current.key).toBe('projects.download');
    render(<Context k="settings.members" />, '/projects/p1/settings');
    expect(state.current.key).toBe('settings.members');
    render(<Context k={null} />, '/projects/p1/settings');
    expect(state.current.key).toBe('settings');
  });

  it('a help link opens the panel at its key until the panel closes', () => {
    render(<HelpLink helpKey="vv.coverage" fragment="intro" label="coverage" />);
    const link = container.querySelector('button.help-link') as HTMLButtonElement;
    expect(link.getAttribute('aria-label')).toBe('Help: coverage');
    act(() => link.click());
    expect(state.open).toBe(true);
    expect(state.current).toEqual({ key: 'vv.coverage', fragment: 'intro' });
    act(() => state.setOpen(false));
    expect(state.current).toEqual({ key: 'vv' });
  });

  it('? outside a field and F1 open help; ? in a field types', () => {
    render(<input aria-label="field" />);
    const input = container.querySelector('input')!;
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: '?', bubbles: true }));
    });
    expect(state.open).toBe(false);
    act(() => {
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: '?', bubbles: true }));
    });
    expect(state.open).toBe(true);
    act(() => state.setOpen(false));
    act(() => {
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'F1', bubbles: true }));
    });
    expect(state.open).toBe(true);
  });

  it('without the manual-help gate: no help links and no shortcut', () => {
    gate(false);
    render(<HelpLink helpKey="vv.coverage" label="coverage" />);
    expect(container.querySelector('button.help-link')).toBeNull();
    act(() => {
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: '?', bubbles: true }));
    });
    expect(state.open).toBe(false);
  });
});

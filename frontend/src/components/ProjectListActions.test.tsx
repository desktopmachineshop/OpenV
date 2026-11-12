import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { ProjectActions } from './ProjectListActions';
import { ProjectListHome } from './ProjectListHome';
import type { WorkspaceHomeData } from './ProjectListHomeLoad';
import { useAppStore } from '../state/store';

// The projects page in the refreshed interface (ui-refresh): a project's
// four glyph buttons are one menu of named actions, and a workspace with no
// activity yet gives the Activity panel no room. Without the key both are as
// they were.

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

const gates = (refresh: boolean) =>
  useAppStore.setState({
    features: {
      channel: 'nightly' as const,
      stable_release: '',
      preview: false,
      features: { 'compact-layout': true, 'ui-refresh': refresh },
    },
  } as never);

const mount = async (ui: React.ReactElement) => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  await act(async () => root.render(<MemoryRouter>{ui}</MemoryRouter>));
};

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const click = (el: Element | null | undefined) =>
  act(async () => {
    el!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });

describe('ProjectActions', () => {
  it('offers the four actions by name from one menu, and keeps the click from the card', async () => {
    const card = vi.fn();
    const calls: string[] = [];
    await mount(
      <div onClick={card}>
        <ProjectActions
          name="Brake ECU"
          menu
          onTemplate={() => calls.push('template')}
          onEdit={() => calls.push('edit')}
          onDownload={() => calls.push('download')}
          onDelete={() => calls.push('delete')}
        />
      </div>
    );
    expect(container.querySelector('.icon-btn')).toBeNull();
    const trigger = container.querySelector('button[aria-label="Actions for Brake ECU"]');
    await click(trigger);
    const labels = Array.from(container.querySelectorAll('[role="menuitem"]')).map((b) => b.textContent);
    expect(labels).toEqual(['Edit project', 'Download', 'Save as template', 'Delete project']);
    await click(Array.from(container.querySelectorAll('[role="menuitem"]')).find((b) => b.textContent === 'Download'));
    expect(calls).toEqual(['download']);
    expect(card).not.toHaveBeenCalled();
  });

  it('keeps the four glyph buttons without the menu', async () => {
    await mount(<ProjectActions name="Brake ECU" menu={false} onTemplate={() => {}} onEdit={() => {}} onDownload={() => {}} onDelete={() => {}} />);
    expect(container.querySelectorAll('.icon-btn')).toHaveLength(4);
  });
});

describe('ProjectListHome with no activity', () => {
  const data: WorkspaceHomeData = {
    todo: [],
    activity: [],
    figures: {},
    proposals: [],
    totals: { artifacts: 0, traced: 0, verified: 0, failing: 0 },
  };
  const home = <ProjectListHome workspace="Acme" projects={1} data={data} phone={false} />;

  it('leaves the empty Activity panel out in the refreshed interface', async () => {
    gates(true);
    await mount(home);
    expect(container.querySelector('#home-activity')).toBeNull();
    expect(container.querySelector('#home-needs')).not.toBeNull();
  });

  it('shows it, saying so, without the key', async () => {
    gates(false);
    await mount(home);
    expect(container.querySelector('#home-activity')).not.toBeNull();
    expect(container.textContent).toContain('No activity yet.');
  });
});
